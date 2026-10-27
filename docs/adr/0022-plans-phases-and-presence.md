# 0022. Plans, phases and who is working on what

## Status

Accepted.

## Context

An agent given a task writes itself an execution plan: steps, checks,
procedures. That plan lives in the agent's conversation. Once the task is
under way nobody sees it again. It cannot be searched, nothing records which
step is done, and a team of three working on one project cannot tell what the
others or their agents are doing, or how far the project really is.
Handoffs (migrations/000010) answer "continue this", not "where
is everyone". Raised by the maintainer's colleagues, 2026-10-27.

The maintainer decided three things on 2026-10-27:

1. Plans are **their own primitive**, not a structured artifact. A tick on a
   checklist has to be cheap, conflict-free and searchable, which an
   encrypted file rewritten per tick is not.
2. **Anyone on the project** may change them. A reader reads; every role that
   can write to the project can tick, add and reassign.
3. **Live position, with time and tokens.** The interface shows, live and
   from anywhere, where each agent currently is. That covers the person
   watching their own agent at home and the colleagues coordinating with it.
   Time spent per step is recorded, and so are tokens when the client can
   report them.

A fourth constraint came with the first: agents under pressure write a lot,
and **the history must not grow without bound**.

## Decision

### The model

```text
project
  └─ phase      ordered; planned → active → done
       └─ plan  draft → active → done | abandoned   (phase optional)
            └─ item   task | check; open → in_progress → done | blocked | skipped
```

- A **phase** is a stretch of the project ("migration", "release 1.2").
- A **plan** is what an agent or a person intends to do: a title, free text,
  and an ordered list of items. A plan may sit outside any phase. An agent
  that just started a task need not decide where it belongs, and a person
  can move it into a phase later.
- An **item** is one step. `task` is work to do. `check` is an
  operational check, something to verify rather than produce, such as
  "migrations run on staging" or "rollback tested". They look different in
  the interface and are otherwise the same.

### What is encrypted (ADR 0011)

Titles, plan text, item text and the "what I am doing" line of presence are
CONTENT PAYLOAD. Status, kind, position, assignee, version and timestamps are
OPERATIONAL METADATA and stay plaintext, so the board can be filtered and
counted without decrypting anything.

**A plan's text and all of its items' text are one encrypted blob on the
plan row.** Item rows carry only metadata. That gives three properties:

- Ticking an item touches one small plaintext row and re-encrypts nothing.
  This is the operation agents do most, and it does not conflict with
  someone editing another item's status.
- Reading a plan is one decrypt, not one per item.
- Changing one item's text or note, or appending items, is merged on the
  server. It locks the plan row, decrypts, changes only that item and
  re-encrypts, all in one transaction. Two agents adding items to the same
  plan at once are serialized, not refused, and neither loses the other's
  items. Only replacing the plan's own title and text takes an expected
  `version`, like an artifact write, because that is the one edit where
  "whose text wins" is a real question.

**Search decrypts.** Content is encrypted, so the database cannot search it.
A search reads the project's plans, decrypts them and matches in Go. That is
bounded by one project's plans, which is hundreds, not millions. Searching
across projects is not offered until something needs it.

### History that does not grow without bound

- **Text edits keep no history.** A plan's text is its current text.
- **Status changes are recorded**, one row per real change: who, which client,
  from what, to what, when. Setting a status an item already has writes
  nothing. Only the **last 20 changes per item** are kept; older ones are
  deleted in the same transaction that adds the 21st. Growth is bounded by
  items × 20, and "who finished this, and when" is always answerable.
- Item ticks write **no audit row and no outbox event**. The audit log is a
  security trail, and a checkbox is not a security event. Creating and
  deleting phases and plans is audited. The interface sees changes by
  polling the board, not through the outbox.

### Presence: where everyone is

`work_presence` holds **one row per client credential**: the project, the
plan and item it is on (both optional), an encrypted one-line "what I am
doing", and when it last reported. Every report overwrites the row, so
presence has no history.

- A report is a heartbeat: an agent calls `plan.report` when it moves to a
  step, and again whenever it likes while working. A row that has not
  reported for **15 minutes** reads as idle, showing since when. Rows idle
  for **7 days** are deleted on the next report anyone makes, the same
  ride-on-the-write pattern as activity retention (migrations/000029).
- A person in the interface has presence too, through their browser
  session's credential, when they mark an item as theirs and in progress.
- Revoking a credential removes its presence row.

### Time and tokens

Effort is **aggregated, never logged**: `plan_item_effort` keeps one row per
(item, principal), holding seconds, tokens in and tokens out.

- **Time** is the time covered by heartbeats. A report on the same item
  within 15 minutes of the previous one adds the gap between them. A longer
  gap adds nothing, because an agent that went quiet for an hour was not
  provably working on it. Moving to another item credits the old item up to
  the move. Work time is therefore a lower bound that an idle or crashed
  agent cannot inflate.
- **Tokens** are reported by the client as deltas since its last report, and
  only if it knows them. A model usually cannot see its own token counts.
  The client can, through Claude Code hooks or a status line, and a wrapper
  can pass them on. They are shown labelled "reported", per item and per
  phase, and are never estimated. A single report is capped, so one bad call
  cannot add a billion.

### Surfaces

- **MCP** (agents):
  - `plan.board`: the state of the project. Phases with progress, open
    plans, items in progress and who has them, and where everyone is.
  - `plan.create`, `plan.get` and `plan.add_items`, for creating, reading
    and extending a plan.
  - `plan.update_item`, which changes status, assignee or note, and
    `plan.report`, the presence heartbeat with optional token deltas.
  - `plan.search` and `phase.create`/`phase.update`.

  `context.bootstrap` gains a short board summary, so an agent starting work
  learns what is done and who is where before it plans anything.
- **HTTP** mirrors the same operations under `/v1/projects/{ns}/{slug}/plans`
  and `/phases`, plus `GET /v1/me/presence`, which shows where everyone is
  across all of the caller's projects, for watching from anywhere.
- **Interface:**
  - a **Plans** tab per project, with phases, plans, items, a live presence
    strip, and time and tokens per item;
  - **"Who is working where"** on the dashboard.

  Both poll every 10 seconds while visible. That is enough to watch an agent
  remotely, works through any proxy, and needs no server push. Push can come
  with Phase 2's subscriptions.

### Authorization

Two project capabilities: `plan.read` for every role, and `plan.write` for
every role that can write artifacts. RLS scopes every table to project
members, exactly as handoffs are scoped. Presence is visible to every
project member. Seeing where your colleagues are is the point, and it shows
only position, never content outside the project.

## Consequences

- Agents get a place to put the plan they already make, and a team gets the
  board it was missing. Nothing changes for a client that never calls
  `plan.*`.
- Plan storage is bounded per plan by the size of its text. The blob is
  capped at 1 MB and counts against the namespace quota (ADR 0021) like
  history does.
- Time and tokens are only as honest as heartbeats and client reports. The
  interface calls them "tracked" and "reported", not "spent".
- A plan's text cannot be searched by SQL, and every search pays for decrypting
  the project's plans. That is the price of ADR 0011, and it is the right one.
- Polling every 10 seconds costs one indexed read per open screen.
  Subscriptions (Phase 2) replace it when they exist.
