# 0033. What changed while you worked: a change feed an agent reads

## Status

Accepted, 2026-11-16.

Builds on [ADR 0032](0032-agent-inbox.md) (the inbox and its Claude Code
hook) and reads what [ADR 0022](0022-plans-phases-and-presence.md) (plans,
transitions) and [ADR 0025](0025-memories.md) (memories) already store.

## Context

Two agents work on one project. One records a gotcha ("staging Ollama needs
the VPN"), marks a step blocked, or opens a handoff. The other learns about
it at its next `context.bootstrap`, which is usually the next session: too
late for the work in progress. The roadmap called this "subscriptions, or
reliable cursor polling, for changes during a session".

MCP gives the server no way to speak first. The interface already has push
(the board streams), but an agent only hears the server when it calls it, or
when a hook on its side asks.

## Decision

**A read, not a push.** `GET /v1/projects/{ns}/{slug}/changes?since=` and
the MCP tool `project.changes` return what everyone but the caller did on
the project after `since`:

- memories recorded (with their kind and title),
- plan steps moved to blocked or done, or out of blocked (with the step's
  text and its plan),
- handoffs opened,
- artifacts written, one line per artifact for its newest version
  ("created" or "updated", with the logical path), *added 2026-11-20*,
- ideas added, proposed, accepted, rejected or withdrawn, each idea's latest
  such move with its title; claims are left out, *added 2026-11-20*.

Newest first, at most 20, reaching back at most a day. The answer carries
the server's `now`, which the next call passes as `since`. Without `since`
it returns nothing and `now`: a starting point. The caller's own changes are
left out, and a kind the caller's role cannot read is left out rather than
refused.

**The Claude Code inbox hook reports it.** With `CC_PROJECT` set, each poll
of the inbox hook also asks for changes and prints a few lines into the
model's context after any messages: who did what, and which tool reads it
(`memory.get`, `plan.get`, `handoff.get`, `artifact.read`, `idea.get`). The first poll of a session only
records the starting point. Other clients call `project.changes` when they
resume work or between larger steps; the agent guide says so.

**Built from what is stored, not from a new event log.** `internal/changes`
composes the memories, plans and ideas services and reads the plaintext
metadata of handoffs (id, status, author) and artifact versions (path,
version, author) — never artifact content. Ideas come from the 20 most
recently updated ones, so a burst of claims can push an older idea's move
out of one read; the next one is about what is new anyway. Every read goes through the owning service's
authorization and decryption. `domain_events` was considered and not used:
it covers only artifacts and handoffs and does not record who acted, so it
cannot leave the caller's own changes out.

**Cheap when nothing moved.** The hook polls every 15 seconds per session.
Step moves are found in the plaintext transition log first, and the
project's plans are decrypted only when one matched. Migration 000046 adds
the two indexes these polls need.

**Commit lag is handled by the client.** A change is stamped when its
transaction starts and visible when it commits, so one can appear just
before a `now` already handed out. The hook asks from 30 seconds before its
previous `now` and keeps the keys of what it printed, so a late commit is
reported once and nothing twice.

## Consequences

- An agent with the hook hears about a colleague's gotcha within one poll
  interval, without being told to look. Without the hook it hears when it
  calls `project.changes`.
- The feed is a pointer, not a log: titles and moves, never bodies. The
  agent reads what touches its task.
- Artifacts, ideas and messages are not in it. Artifacts change through
  handoffs in practice, ideas wait on people rather than agents, and
  messages have the inbox.
- Not done: server push to agents (MCP has no channel a client surfaces to
  the model mid-turn), and Codex CLI, which has no hook that adds context
  mid-turn.
