#!/usr/bin/env python3
"""Hand messages from people to a Claude Code agent while it works.

A Claude Code `PostToolUse` and `UserPromptSubmit` hook. A person on the
project can send this session a message from the Context Commander interface
(docs/adr/0032). MCP gives the server no way to speak first, so the message
waits in the session's inbox. This script polls the inbox after tool calls
and before each prompt, and when something is waiting it prints it as
`hookSpecificOutput.additionalContext`, which Claude Code adds to the
model's context straight away. It then acknowledges what it printed in full, so the
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
  CC_PROJECT         optional, the project path ("namespace/slug"). With it,
                     each poll also reports what everyone else did on that
                     project since the previous poll: memories recorded,
                     steps blocked, finished or unblocked, handoffs opened
                     (GET /v1/projects/{ns}/{slug}/changes, docs/adr/0033).
  CC_CHANGES         set to 0 to turn that report off.

Standard library only. It never slows the agent down or fails the tool call:
it polls at most once per interval, gives the server a short timeout, and on
any problem prints nothing and exits 0. A server that is down costs one
short timeout per interval, and nothing else.

See deploy/mcp-clients/README.md, "Messages to the agent".
"""

import json
from datetime import datetime, timedelta
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


def changes_file(session_id: str) -> Path:
    return state_file(session_id).with_suffix(".changes.json")


# How far before the previous poll's `now` the next one asks from. A change
# is stamped when its transaction starts and visible when it commits, so one
# committed just after a poll can carry a time just before it. The overlap
# catches it; the ids already printed keep it from showing twice.
OVERLAP_SECONDS = 30
# The report is a pointer to what moved, not a log: it gets a small share of
# the context budget, after messages.
CHANGES_BUDGET = 2500


def change_key(c: dict) -> str:
    return f"{c.get('kind')}:{c.get('id')}:{c.get('detail')}:{c.get('at')}"


def describe(c: dict) -> str:
    who = c.get("by_name") or "someone"
    kind, detail, title = c.get("kind"), c.get("detail", ""), c.get("title", "")
    if kind == "memory":
        return f"- {who} recorded a {detail}: \"{title}\" (memory.get {c.get('id')})"
    if kind == "item":
        where = f" in plan \"{c['where']}\"" if c.get("where") else ""
        verb = {"blocked": "marked a step blocked", "done": "finished a step", "unblocked": "unblocked a step"}.get(detail, f"moved a step to {detail}")
        return f"- {who} {verb}{where}: \"{title}\" (plan.get {c.get('plan_id', '')})"
    if kind == "handoff":
        return f"- {who} opened a handoff (handoff.get {c.get('id')})"
    return f"- {who}: {kind} {detail} {title}".rstrip()


def render_changes(project: str, items: list) -> str:
    lines = [
        f"Context Commander: since this session last checked, other people and agents changed "
        f"{project}. Read what touches your task with the tool named on each line."
    ]
    used = len(lines[0])
    for k, c in enumerate(items):
        line = describe(c)
        if used + len(line) + 1 > CHANGES_BUDGET:
            lines.append(f"- … and {len(items) - k} more (project.changes)")
            break
        lines.append(line)
        used += len(line) + 1
    return "\n".join(lines)


def poll_changes(base: str, token: str, timeout: float, session_id: str) -> str:
    """The report of what others changed since the previous poll, or "".

    The first poll of a session only records where to start from: what
    happened before the session is context.bootstrap's to tell."""
    project = os.environ.get("CC_PROJECT", "").strip().strip("/")
    if not project or os.environ.get("CC_CHANGES", "1") == "0" or project.count("/") != 1:
        return ""
    ns, slug = (urllib.parse.quote(p, safe="") for p in project.split("/"))
    path = changes_file(session_id)
    try:
        state = json.loads(path.read_text())
    except (OSError, ValueError):
        state = {}
    url = f"{base}/v1/projects/{ns}/{slug}/changes?"
    since = state.get("since")
    if not since:
        # The session's starting point. What falls in the overlap before it
        # happened before the session too: mark it seen, print nothing.
        since = request("GET", url, token, timeout).get("now")
        if not since:
            return ""
        state = {"since": since, "seen": []}
        first = True
    else:
        first = False
    moment = datetime.fromisoformat(since.replace("Z", "+00:00")) - timedelta(seconds=OVERLAP_SECONDS)
    answer = request("GET", url + urllib.parse.urlencode({"since": moment.isoformat().replace("+00:00", "Z")}), token, timeout)
    items = [c for c in answer.get("items", []) if isinstance(c, dict)]
    seen = set(state.get("seen", []))
    fresh = [c for c in items if change_key(c) not in seen]
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({"since": answer.get("now"), "seen": [change_key(c) for c in items][:200]}))
    except OSError:
        pass
    if first or not fresh:
        return ""
    return render_changes(project, fresh)


def request(method: str, url: str, token: str, timeout: float, body=None) -> dict:
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Authorization": f"Bearer {token}"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return json.load(response)


AUDIENCE = {"session": "to you", "principal": "to all your sessions", "project": "to everyone working on the project"}


# Claude Code caps additionalContext at 10,000 characters; past that it saves
# the text to a file and shows the model only a preview and a path, without
# asking it to read the file. A message cut off there would be acknowledged
# and never read, so the hook stays under the cap and acknowledges only what
# it printed in full. The rest waits for the next poll.
BUDGET = 9000
# A message too long to print whole is shown cut, left unacknowledged, and the
# agent is told where the full text is.
EXCERPT = 6000


def header(m: dict) -> str:
    sender = m.get("from_name") or m.get("from") or "someone"
    audience = AUDIENCE.get(m.get("audience"), "")
    return f"--- Message {m.get('id', '')} from {sender} ({audience}) in {m.get('project', '?')}, sent {m.get('created_at', '')} ---\n"


def render(items: list, budget: int = BUDGET) -> tuple[str, list]:
    """The context to print and the ids printed in full (to acknowledge).

    Written as a statement of what arrived, not as an instruction: the
    Claude Code documentation warns that text framed as an out-of-band
    system command can trigger the model's prompt-injection defenses, and
    these messages are ordinary input from a person on the project."""
    intro = (
        "Context Commander delivered messages that people on this project sent to this "
        "session while it was working. Each header below was written by the server and "
        "names the person who sent the message; the message text follows it."
    )
    parts, done, used = [intro], [], len(intro)
    for m in items:
        body = str(m.get("body", ""))
        whole = header(m) + body
        if used + len(whole) + 2 <= budget:
            parts.append(whole)
            done.append(m["id"])
            used += len(whole) + 2
            continue
        if not done:
            # Nothing fits whole yet: show this one cut, and leave it unacknowledged.
            room = max(0, min(EXCERPT, budget - used - 400))
            parts.append(
                header(m)
                + body[:room]
                + f"\n[The message is longer than fits here ({len(body)} characters). "
                f"The full text is available from the inbox.next tool; it is not acknowledged yet.]"
            )
        remaining = len(items) - len(done) - (0 if done else 1)
        if remaining > 0:
            parts.append(f"[{remaining} more message(s) are waiting and will be delivered after the next tool call, or now through inbox.next.]")
        break
    return "\n\n".join(parts), done


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
    try:
        report = poll_changes(base, token, timeout, hook.get("session_id") or "")
    except (OSError, ValueError):
        report = ""  # the inbox still gets through when the changes cannot be read
    # Messages and the report share one budget under Claude Code's cap.
    text, printed = render(items, BUDGET - len(report) - 2) if items else ("", [])
    text = "\n\n".join(t for t in (text, report) if t)
    if not text:
        return
    print(json.dumps({"hookSpecificOutput": {"hookEventName": event, "additionalContext": text}}))
    sys.stdout.flush()
    if not printed:
        return
    # Acknowledged after printing: if the ack fails, the next poll offers
    # the messages again, which is better than a message that was never seen.
    try:
        request("POST", base + "/v1/me/inbox/ack", token, timeout, {"message_ids": printed})
    except (OSError, ValueError):
        pass


if __name__ == "__main__":
    try:
        main()
    except Exception:  # pylint: disable=broad-except
        pass  # silent, whatever went wrong: a hook must never get in the agent's way
    sys.exit(0)
