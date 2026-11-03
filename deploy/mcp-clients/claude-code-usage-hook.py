#!/usr/bin/env python3
"""Send what each Claude Code turn cost to Context Commander, per model.

A Claude Code `Stop` hook. Claude Code writes every response's token usage and
model to the session transcript; a model cannot see its own usage, so this is
the one place the counts are exact. After each turn this script sums the
transcript by model, subtracts what it already sent for the session, and posts
the difference to `POST /v1/me/usage`, one request per model. The server
credits it to the plan step this client last reported with `plan.report`
(docs/adr/0022), and drops it when the client never said where it is: tokens
are never attached to a guessed step.

Subagents count too. Claude Code writes a subagent's turns to its own
transcript, `<session>/subagents/agent-*.jsonl` beside the session's
`<session>.jsonl`; older versions wrote them into the session transcript
marked `isSidechain`. Both are read, and a response that appears in both is
counted once.

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
import re
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

# What the server accepts as a model label (internal/plans.ValidModel).
# Anything else, such as Claude Code's "<synthetic>" placeholder, is sent as
# "not stated" rather than refused.
MODEL = re.compile(r"[A-Za-z0-9._:/@+-]{1,128}")


def transcripts(transcript: Path) -> list:
    """The session transcript and its subagents' transcripts."""
    out = [transcript]
    subagents = transcript.with_suffix("") / "subagents"
    if subagents.is_dir():
        out.extend(sorted(subagents.glob("*.jsonl")))
    return out


def totals(files: list) -> dict:
    """Sum usage over the transcripts' distinct responses, by model.

    One response is written several times as it streams, each copy carrying
    the same message id and the same usage; counting copies would multiply
    the total. The last copy of each id wins, across all the files.
    """
    by_id = {}
    for path in files:
        try:
            f = path.open(encoding="utf-8")
        except OSError:
            continue  # a subagent transcript removed while we looked
        with f:
            for line in f:
                try:
                    entry = json.loads(line)
                except ValueError:
                    continue
                if not isinstance(entry, dict) or entry.get("type") != "assistant":
                    continue
                message = entry.get("message")
                if not isinstance(message, dict) or not isinstance(message.get("usage"), dict):
                    continue
                key = message.get("id") or entry.get("uuid")
                if key:
                    model = message.get("model")
                    by_id[key] = (model if isinstance(model, str) and MODEL.fullmatch(model) else "", message["usage"])
    out = {}
    for model, usage in by_id.values():
        counts = out.setdefault(model, {field: 0 for field in FIELDS})
        for field, source in FIELDS.items():
            value = usage.get(source)
            if isinstance(value, int) and value > 0:
                counts[field] += value
    return {model: counts for model, counts in out.items() if any(counts.values())}


def state_file(session_id: str) -> Path:
    base = os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local" / "state")
    safe = "".join(c for c in session_id if c.isalnum() or c in "-_") or "unknown"
    return Path(base) / "context-commander" / "usage" / f"{safe}.json"


def read_sent(state: Path, now: dict) -> dict:
    """What was already sent, by model.

    A state file from before models were sent holds one flat total. Those
    tokens were sent; which model they were for was not. They are taken off
    this session's models in order, so none of them is sent a second time.
    """
    try:
        raw = json.loads(state.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    if not isinstance(raw, dict):
        return {}
    if isinstance(raw.get("models"), dict):
        return {m: {f: int(c.get(f, 0)) for f in FIELDS} for m, c in raw["models"].items() if isinstance(c, dict)}
    sent = {}
    for field in FIELDS:
        left = int(raw.get(field, 0) or 0)
        for model in sorted(now):
            take = min(left, now[model][field])
            sent.setdefault(model, {f: 0 for f in FIELDS})[field] = take
            left -= take
    return sent


def write_sent(state: Path, sent: dict) -> None:
    state.parent.mkdir(parents=True, exist_ok=True)
    state.write_text(json.dumps({"models": sent}), encoding="utf-8")


def post(url: str, token: str, body: dict) -> dict:
    request = urllib.request.Request(
        url,
        data=json.dumps(body).encode(),
        method="POST",
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.load(response)


def main() -> None:
    hook = json.load(sys.stdin)
    transcript = hook.get("transcript_path")
    session_id = hook.get("session_id") or ""
    token = os.environ.get("CC_TOKEN", "")
    if not transcript or not token:
        return
    url = os.environ.get("CC_URL", "http://127.0.0.1:8490").rstrip("/") + "/v1/me/usage"

    now = totals(transcripts(Path(transcript)))
    state = state_file(session_id)
    sent = read_sent(state, now)
    dropped = False
    for model in sorted(now):
        before = sent.get(model, {f: 0 for f in FIELDS})
        delta = {f: max(0, now[model][f] - before.get(f, 0)) for f in FIELDS}
        if not any(delta.values()):
            continue
        answer = post(url, token, {"model": model, **delta})
        # Recorded as sent even when the server dropped it for want of a
        # step: sending the same tokens again on the next turn would credit
        # them to whatever step comes next, which is the guess this is built
        # to avoid. Written after every model, so a failure halfway loses
        # nothing already sent and re-sends nothing.
        sent[model] = {f: max(before.get(f, 0), now[model][f]) for f in FIELDS}
        write_sent(state, sent)
        dropped = dropped or not answer.get("credited")
    if dropped:
        print("context-commander: usage not credited — this client has not reported a step (plan.report)", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, urllib.error.URLError) as err:
        print(f"context-commander usage hook: {err}", file=sys.stderr)
    sys.exit(0)
