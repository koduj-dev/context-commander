#!/usr/bin/env python3
"""Send what each Codex CLI turn cost to Context Commander, per model.

The Codex counterpart of claude-code-usage-hook.py. Codex CLI writes every
session to a rollout file, `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl`
(CODEX_HOME defaults to ~/.codex). After each model response the rollout gets
a `token_count` event carrying the session's running total, and every turn
starts with a `turn_context` naming the model. This script walks the rollout,
credits each growth of the running total to the model in effect, subtracts
what it already sent for the session, and posts the difference to
`POST /v1/me/usage`, one request per model. The server credits it to the plan
step this client last reported with `plan.report` (docs/adr/0022), and drops
it when the client never said where it is.

OpenAI counts cached input inside input_tokens; this server keeps fresh input
and cache reads apart because they are priced apart, so fresh input is sent
as input_tokens minus cached_input_tokens (and minus cache_write_input_tokens,
which newer rollouts carry). Reasoning tokens are already inside
output_tokens.

Run it either way Codex offers:

  - as a `Stop` hook (~/.codex/hooks.json): the hook's JSON arrives on stdin
    with session_id, transcript_path and model;
  - as the `notify` program (config.toml): the JSON arrives as the last
    argument, with "thread-id", and the rollout is found by that id.

Configuration, from the environment Codex runs in:

  CC_TOKEN   the same personal access token the MCP connection uses
             (bearer_token_env_var in codex-config.toml). It has to be the same
             one: presence is per credential.
  CC_URL     the server, e.g. https://cc.example.com (default
             http://127.0.0.1:8490).

Standard library only, and it never fails the turn: any problem is written to
stderr and the script exits 0.

See deploy/mcp-clients/README.md, "Token usage".
"""

import json
import os
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

FIELDS = ("tokens_in", "tokens_out", "cache_read_tokens", "cache_write_tokens")
OPENAI = ("input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens")

# What the server accepts as a model label (internal/plans.ValidModel).
MODEL = re.compile(r"[A-Za-z0-9._:/@+-]{1,128}")


def label(model) -> str:
    return model if isinstance(model, str) and MODEL.fullmatch(model) else ""


def find_rollout(session_id: str) -> "Path | None":
    home = Path(os.environ.get("CODEX_HOME") or Path.home() / ".codex")
    safe = "".join(c for c in session_id if c.isalnum() or c == "-")
    if not safe:
        return None
    found = sorted((home / "sessions").glob(f"*/*/*/rollout-*-{safe}.jsonl"))
    return found[-1] if found else None


def convert(delta: dict) -> dict:
    """OpenAI's counts as this server's four."""
    cached = delta["cached_input_tokens"]
    written = delta["cache_write_input_tokens"]
    return {
        "tokens_in": max(0, delta["input_tokens"] - cached - written),
        "tokens_out": delta["output_tokens"],
        "cache_read_tokens": cached,
        "cache_write_tokens": written,
    }


def totals(rollout: Path, default_model: str) -> dict:
    """The session's tokens, by model.

    Each token_count carries the running total. Its growth since the previous
    one is what the last response cost, and goes to the model of the turn it
    was in. Repeated events with the same total add nothing; a total that went
    down (a rollout resumed into a fresh count) starts over from zero rather
    than going negative.
    """
    model = default_model
    previous = {k: 0 for k in OPENAI}
    out = {}
    with rollout.open(encoding="utf-8") as f:
        for line in f:
            try:
                entry = json.loads(line)
            except ValueError:
                continue
            if not isinstance(entry, dict) or not isinstance(entry.get("payload"), dict):
                continue
            payload = entry["payload"]
            if entry.get("type") == "turn_context":
                model = label(payload.get("model")) or model
                continue
            if entry.get("type") != "event_msg" or payload.get("type") != "token_count":
                continue
            info = payload.get("info")
            total = info.get("total_token_usage") if isinstance(info, dict) else None
            if not isinstance(total, dict):
                continue
            current = {k: total.get(k) if isinstance(total.get(k), int) and total.get(k) > 0 else 0 for k in OPENAI}
            if any(current[k] < previous[k] for k in OPENAI):
                previous = {k: 0 for k in OPENAI}
            delta = convert({k: current[k] - previous[k] for k in OPENAI})
            previous = current
            counts = out.setdefault(model, {field: 0 for field in FIELDS})
            for field in FIELDS:
                counts[field] += delta[field]
    return {m: c for m, c in out.items() if any(c.values())}


def state_file(session_id: str) -> Path:
    base = os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local" / "state")
    safe = "".join(c for c in session_id if c.isalnum() or c in "-_") or "unknown"
    return Path(base) / "context-commander" / "usage" / f"codex-{safe}.json"


def read_sent(state: Path) -> dict:
    try:
        raw = json.loads(state.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    models = raw.get("models") if isinstance(raw, dict) else None
    if not isinstance(models, dict):
        return {}
    return {m: {f: int(c.get(f, 0)) for f in FIELDS} for m, c in models.items() if isinstance(c, dict)}


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


def hook_input() -> dict:
    """The event, from notify's last argument or a hook's stdin."""
    if len(sys.argv) > 1:
        return json.loads(sys.argv[-1])
    return json.load(sys.stdin)


def main() -> None:
    event = hook_input()
    if not isinstance(event, dict):
        return
    if "type" in event and event.get("type") != "agent-turn-complete":
        return  # a notify event that is not the end of a turn
    session_id = event.get("session_id") or event.get("thread-id") or ""
    token = os.environ.get("CC_TOKEN", "")
    if not session_id or not token:
        return
    url = os.environ.get("CC_URL", "http://127.0.0.1:8490").rstrip("/") + "/v1/me/usage"

    rollout = Path(event["transcript_path"]) if event.get("transcript_path") else find_rollout(session_id)
    if rollout is None or not rollout.is_file():
        return
    now = totals(rollout, label(event.get("model")))
    state = state_file(session_id)
    sent = read_sent(state)
    dropped = False
    for model in sorted(now):
        before = sent.get(model, {f: 0 for f in FIELDS})
        delta = {f: max(0, now[model][f] - before.get(f, 0)) for f in FIELDS}
        if not any(delta.values()):
            continue
        answer = post(url, token, {"model": model, **delta})
        # Recorded as sent even when dropped for want of a step, and after
        # every model: the same never-re-send rule as the Claude Code hook.
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
