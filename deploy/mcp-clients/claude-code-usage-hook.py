#!/usr/bin/env python3
"""Send what each Claude Code turn cost to Context Commander.

A Claude Code `Stop` hook. Claude Code writes every response's token usage to
the session transcript; a model cannot see its own usage, so this is the one
place the counts are exact. After each turn this script sums the transcript,
subtracts what it already sent for the session, and posts the difference to
`POST /v1/me/usage`. The server credits it to the plan step this client last
reported with `plan.report` (docs/adr/0022), and drops it when the client
never said where it is: tokens are never attached to a guessed step.

Configuration, from the environment Claude Code runs in:

  CC_TOKEN   the same personal access token the MCP connection uses. It has
             to be the same one: presence is per credential, so a different
             token would be a different client that is nowhere.
  CC_URL     the server, e.g. https://cc.example.com (default
             http://127.0.0.1:8490).

Standard library only, and it never fails the turn: any problem is written to
stderr and the script exits 0, because a hook that breaks Claude Code to
report a statistic has its priorities backwards.

See deploy/mcp-clients/README.md, "Token usage".
"""

import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

FIELDS = {
    "tokens_in": "input_tokens",
    "tokens_out": "output_tokens",
    "cache_read_tokens": "cache_read_input_tokens",
    "cache_write_tokens": "cache_creation_input_tokens",
}


def totals(transcript: Path) -> dict:
    """Sum usage over the transcript's distinct responses.

    One response is written several times as it streams, each copy carrying
    the same message id and the same usage; counting copies would multiply
    the total. The last copy of each id wins.
    """
    by_id = {}
    with transcript.open(encoding="utf-8") as f:
        for line in f:
            try:
                entry = json.loads(line)
            except ValueError:
                continue
            if entry.get("type") != "assistant":
                continue
            message = entry.get("message")
            if not isinstance(message, dict) or not isinstance(message.get("usage"), dict):
                continue
            key = message.get("id") or entry.get("uuid")
            if key:
                by_id[key] = message["usage"]
    out = {field: 0 for field in FIELDS}
    for usage in by_id.values():
        for field, source in FIELDS.items():
            value = usage.get(source)
            if isinstance(value, int) and value > 0:
                out[field] += value
    return out


def state_file(session_id: str) -> Path:
    base = os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local" / "state")
    safe = "".join(c for c in session_id if c.isalnum() or c in "-_") or "unknown"
    return Path(base) / "context-commander" / "usage" / f"{safe}.json"


def main() -> None:
    hook = json.load(sys.stdin)
    transcript = hook.get("transcript_path")
    session_id = hook.get("session_id") or ""
    token = os.environ.get("CC_TOKEN", "")
    if not transcript or not token:
        return
    url = os.environ.get("CC_URL", "http://127.0.0.1:8490").rstrip("/") + "/v1/me/usage"

    now = totals(Path(transcript))
    state = state_file(session_id)
    try:
        sent = json.loads(state.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        sent = {}
    delta = {field: max(0, now[field] - int(sent.get(field, 0))) for field in FIELDS}
    if not any(delta.values()):
        return

    request = urllib.request.Request(
        url,
        data=json.dumps(delta).encode(),
        method="POST",
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        answer = json.load(response)

    # Recorded as sent even when the server dropped it for want of a step:
    # sending the same tokens again on the next turn would credit them to
    # whatever step comes next, which is the guess this is built to avoid.
    state.parent.mkdir(parents=True, exist_ok=True)
    state.write_text(json.dumps(now), encoding="utf-8")
    if not answer.get("credited"):
        print("context-commander: usage not credited — this client has not reported a step (plan.report)", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, urllib.error.URLError) as err:
        print(f"context-commander usage hook: {err}", file=sys.stderr)
    sys.exit(0)
