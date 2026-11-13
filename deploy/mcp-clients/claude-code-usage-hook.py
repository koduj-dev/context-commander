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
  CC_PROJECT optional, the project path ("namespace/slug") this session
             works in. With it, a session that never reported a step is
             still counted in the project's usage instead of dropped.

Standard library only, and it never fails the turn: any problem is written to
stderr and the script exits 0, because a hook that breaks Claude Code to
report a statistic has its priorities backwards.

See deploy/mcp-clients/README.md, "Token usage".
"""

import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

FIELDS = {
    "tokens_in": "input_tokens",
    "tokens_out": "output_tokens",
    "cache_read_tokens": "cache_read_input_tokens",
    "cache_write_tokens": "cache_creation_input_tokens",
    # Nested: usage.cache_creation.ephemeral_1h_input_tokens. The part of
    # the cache writes made to the one-hour cache, which costs more than the
    # five-minute one; the server keeps it as a subset of cache_write_tokens
    # (docs/adr/0031).
    "cache_write_1h_tokens": ("cache_creation", "ephemeral_1h_input_tokens"),
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


def responses(files: list) -> dict:
    """The transcripts' distinct responses: id -> (model, counts).

    One response is written several times as it streams, each copy carrying
    the same message id and the same usage; counting copies would multiply
    the total. The last copy of each id wins, across all the files, and the
    order is the order in which each id first appeared.
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
                if not key:
                    continue
                model = message.get("model")
                usage = message["usage"]
                counts = {}
                for field, source in FIELDS.items():
                    if isinstance(source, tuple):
                        parent = usage.get(source[0])
                        value = parent.get(source[1]) if isinstance(parent, dict) else None
                    else:
                        value = usage.get(source)
                    counts[field] = value if isinstance(value, int) and value > 0 else 0
                # A subset of the cache writes, whatever the transcript says.
                counts["cache_write_1h_tokens"] = min(counts["cache_write_1h_tokens"], counts["cache_write_tokens"])
                by_id[key] = (model if isinstance(model, str) and MODEL.fullmatch(model) else "", counts)
    return by_id


def totals(files: list) -> dict:
    """Sum usage over the transcripts' distinct responses, by model."""
    out = {}
    for model, counts in responses(files).values():
        acc = out.setdefault(model, {field: 0 for field in FIELDS})
        for field in FIELDS:
            acc[field] += counts[field]
    return {model: acc for model, acc in out.items() if any(acc.values())}


def state_file(session_id: str) -> Path:
    base = os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local" / "state")
    safe = "".join(c for c in session_id if c.isalnum() or c in "-_") or "unknown"
    return Path(base) / "context-commander" / "usage" / f"{safe}.json"


def read_sent(state: Path, transcript: Path) -> dict:
    """What was already sent, by model.

    A state file from before models were sent holds one flat total, of the
    session transcript only: that version read no subagent transcripts. The
    transcript is append-only, so what it sent was its responses up to some
    point, in order. Walking them in that order and taking each one's counts
    off the old total recovers which models those tokens were for: tokens it
    sent are not sent again, and tokens it never saw (a later response, a
    subagent) are sent under their own model. The recovered baseline is
    saved at once, so the conversion happens exactly once even if nothing is
    posted this turn.
    """
    try:
        raw = json.loads(state.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    if not isinstance(raw, dict):
        return {}
    if isinstance(raw.get("models"), dict):
        return {m: {f: int(c.get(f, 0)) for f in FIELDS} for m, c in raw["models"].items() if isinstance(c, dict)}
    left = {f: int(raw.get(f, 0) or 0) for f in FIELDS}
    sent = {}
    for model, counts in responses([transcript]).values():
        if not any(left.values()):
            break
        acc = sent.setdefault(model, {f: 0 for f in FIELDS})
        for f in FIELDS:
            take = min(left[f], counts[f])
            acc[f] += take
            left[f] -= take
    write_sent(state, sent)
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


def work_context(cwd) -> dict:
    """The branch the turn ran on and the project this hook is set up for.

    The branch is read from the repository in the session's working
    directory, so the server can group usage by it (docs/adr/0027). A
    directory that is not a repository, a detached HEAD, or no git at all
    leave it out. CC_PROJECT ("namespace/slug") lets the server count a
    session that never reported a step instead of dropping its tokens.
    """
    out = {}
    project = os.environ.get("CC_PROJECT", "").strip()
    if project:
        out["project"] = project
    if cwd:
        try:
            branch = subprocess.run(
                ["git", "-C", str(cwd), "branch", "--show-current"],
                capture_output=True, text=True, timeout=2, check=False,
            ).stdout.strip()
        except (OSError, subprocess.SubprocessError):
            branch = ""
        if branch:
            out["branch"] = branch[:200]
    return out


def main() -> None:
    hook = json.load(sys.stdin)
    transcript = hook.get("transcript_path")
    session_id = hook.get("session_id") or ""
    token = os.environ.get("CC_TOKEN", "")
    if not transcript or not token:
        return
    url = os.environ.get("CC_URL", "http://127.0.0.1:8490").rstrip("/") + "/v1/me/usage"

    context = work_context(hook.get("cwd"))
    now = totals(transcripts(Path(transcript)))
    state = state_file(session_id)
    sent = read_sent(state, Path(transcript))
    dropped = False
    for model in sorted(now):
        before = sent.get(model, {f: 0 for f in FIELDS})
        delta = {f: max(0, now[model][f] - before.get(f, 0)) for f in FIELDS}
        # A state file written before one-hour writes were sent has none of
        # them as sent, so the whole session's one-hour writes would come
        # due at once beside only this turn's cache writes. The server
        # refuses a one-hour part larger than the whole; send at most the
        # whole, and the rest is never sent rather than sent wrong.
        delta["cache_write_1h_tokens"] = min(delta["cache_write_1h_tokens"], delta["cache_write_tokens"])
        if not any(delta.values()):
            continue
        answer = post(url, token, {"model": model, **delta, **context})
        # Recorded as sent even when the server dropped it for want of a
        # step: sending the same tokens again on the next turn would credit
        # them to whatever step comes next, which is the guess this is built
        # to avoid. Written after every model, so a failure halfway loses
        # nothing already sent and re-sends nothing.
        sent[model] = {f: max(before.get(f, 0), now[model][f]) for f in FIELDS}
        write_sent(state, sent)
        dropped = dropped or not answer.get("credited")
    if dropped:
        print("context-commander: usage not credited — this client has not reported a step (plan.report) and CC_PROJECT is not set", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, urllib.error.URLError) as err:
        print(f"context-commander usage hook: {err}", file=sys.stderr)
    sys.exit(0)
