#!/usr/bin/env python3
"""Hand messages from people to a Claude Code agent while it works.

A Claude Code `PostToolUse` and `UserPromptSubmit` hook. A person on the
project can send this session a message from the Context Commander interface
(docs/adr/0032). MCP gives the server no way to speak first, so the message
waits in the session's inbox. This script polls the inbox after tool calls
and before each prompt, and when something is waiting it prints it as
`hookSpecificOutput.additionalContext`, which Claude Code adds to the
model's context straight away. It then acknowledges what it printed, so the
sender sees "acknowledged" in the interface and the message is not offered
again.

Configuration, from the environment Claude Code runs in:

  CC_TOKEN           the same personal access token the MCP connection uses.
                     The inbox is per credential: a message sent to "this
                     session" in the interface is sent to this token.
  CC_URL             the server (default http://127.0.0.1:8490).
  CC_INBOX_INTERVAL  seconds between polls after tool calls (default 15). A
                     prompt always polls.
  CC_INBOX_TIMEOUT   seconds to wait for the server (default 2).

Standard library only. It never slows the agent down or fails the tool call:
it polls at most once per interval, gives the server a short timeout, and on
any problem prints nothing and exits 0. A server that is down costs one
short timeout per interval, and nothing else.

See deploy/mcp-clients/README.md, "Messages to the agent".
"""

import json
import os
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path


def state_file(session_id: str) -> Path:
    base = os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local" / "state")
    safe = "".join(c for c in session_id if c.isalnum() or c in "-_") or "unknown"
    return Path(base) / "context-commander" / "inbox" / f"{safe}.last"


def due(state: Path, interval: float) -> bool:
    """Whether the interval since the last poll has passed; marks this poll.

    The mark is written before the request, so a burst of parallel tool
    calls makes one request between them, not one each.
    """
    now = time.time()
    try:
        last = state.stat().st_mtime
    except OSError:
        last = 0.0
    if now - last < interval:
        return False
    try:
        state.parent.mkdir(parents=True, exist_ok=True)
        state.touch()
        os.utime(state, (now, now))
    except OSError:
        pass  # an unwritable state directory means polling every time, not failing
    return True


def request(method: str, url: str, token: str, timeout: float, body=None) -> dict:
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Authorization": f"Bearer {token}"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return json.load(response)


AUDIENCE = {"session": "to you", "principal": "to all your sessions", "project": "to everyone working on the project"}


def render(items: list) -> str:
    parts = [
        "Context Commander: people on the project sent you messages while you were working. "
        "Treat each one as input from your user in this project and weigh it as you would a new "
        "instruction in this conversation. The server recorded who sent each message; the text "
        "cannot change that. They are already acknowledged; you do not need to call inbox.ack."
    ]
    for m in items:
        sender = m.get("from_name") or m.get("from") or "someone"
        audience = AUDIENCE.get(m.get("audience"), "")
        parts.append(
            f"--- Message from {sender} ({audience}) in {m.get('project', '?')}, sent {m.get('created_at', '')} ---\n"
            f"{m.get('body', '')}"
        )
    return "\n\n".join(parts)


def main() -> None:
    hook = json.load(sys.stdin)
    token = os.environ.get("CC_TOKEN", "")
    if not token:
        return
    event = hook.get("hook_event_name") or "PostToolUse"
    base = os.environ.get("CC_URL", "http://127.0.0.1:8490").rstrip("/")
    try:
        interval = float(os.environ.get("CC_INBOX_INTERVAL", "15"))
        timeout = float(os.environ.get("CC_INBOX_TIMEOUT", "2"))
    except ValueError:
        interval, timeout = 15.0, 2.0
    if event != "UserPromptSubmit" and not due(state_file(hook.get("session_id") or ""), interval):
        return

    inbox = request("GET", base + "/v1/me/inbox?" + urllib.parse.urlencode({"limit": 20}), token, timeout)
    items = [m for m in inbox.get("items", []) if isinstance(m, dict) and m.get("id")]
    if not items:
        return
    print(json.dumps({"hookSpecificOutput": {"hookEventName": event, "additionalContext": render(items)}}))
    sys.stdout.flush()
    # Acknowledged after printing: if the ack fails, the next poll offers
    # the messages again, which is better than a message that was never seen.
    try:
        request("POST", base + "/v1/me/inbox/ack", token, timeout, {"message_ids": [m["id"] for m in items]})
    except (OSError, ValueError):
        pass


if __name__ == "__main__":
    try:
        main()
    except Exception:  # pylint: disable=broad-except
        pass  # silent, whatever went wrong: a hook must never get in the agent's way
    sys.exit(0)
