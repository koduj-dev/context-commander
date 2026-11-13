# 0032. Messages to agents mid-work: an inbox per session

## Status

Accepted, 2026-11-13.

Builds on [ADR 0022](0022-plans-phases-and-presence.md) (presence, the board
stream) and reuses the encryption pattern of
[ADR 0025](0025-memories.md). The "a person, not their token" rule is the
one [ADR 0026](0026-idea-stack.md) uses for accepting a proposal.

## Context

The maintainer watches an agent work on the board and a thought comes to
him: "use the staging database", "don't touch billing today", "the bug is in
the cache, not the parser". Today the only way to say it is to wait for the
agent's next prompt, at the terminal it runs in. A colleague watching from
elsewhere cannot say it at all.

MCP is pull-based. The server answers tool calls and has no way to speak
first. Whatever is sent has to wait until the agent next calls the server,
or until something on the agent's side asks for it.

## Decision

### Who a message is for

Presence already names the unit people see on the board: **one row per
client credential** (`work_presence.client_id`). That is what "a session"
means here. A message is addressed to one of three audiences:

| Audience | Columns set | Reaches |
|---|---|---|
| one session | `recipient_client` (and `recipient_principal`, its owner) | that credential only |
| one principal | `recipient_principal` | every credential of that principal |
| the project | neither | every credential of every project member |

There is no link from a user to "their" agents. A service principal records
who registered it (`service_principals.created_by`, migration 000026), and
that migration says on purpose that it is not ownership. So "a user's
agents" is not an audience. The interface offers the sessions presence
shows, plus "everyone working on this project". The principal audience
exists for the case where a person runs several agents on one token, and
for a caller of the HTTP API.

A message to a session also stores that session's principal. That keeps the
RLS policy free of subqueries: "addressed to you" is
`recipient_principal = current principal`.

A browser session is never a recipient. `client_type = 'session'` is refused
as a target, the interface offers no "Message" on its row, and nothing polls
an inbox from a browser. A broadcast is not offered back to the credential
that sent it.

**Accepted limitation.** A session is a credential. Two Claude Code
processes started with the same token share one inbox, and whichever polls
first takes a message addressed to "that session". The deployment guide
already says to create one token per client and machine, for presence's
sake. That advice now matters for messages too.

### The model

`agent_messages` (migration 000043): project, sender principal and
credential, the recipient columns above, the body, `created_at`,
`expires_at`, and a `seq` (bigserial) for the inbox cursor.

- **The body is CONTENT PAYLOAD** (ADR 0011). It is one encrypted blob per
  row, `ResourceType "agent_message"`, version 1. A message is never edited.
  Sender, recipients and times are OPERATIONAL METADATA.
- **At most 8 KiB.** A message is a thought, not a document. Something
  longer belongs in an artifact that the message points to.
- **Expiry is required.** The default is 24 hours, and a sender may choose
  from 1 minute to 30 days. A thought sent mid-work is about that work. A
  day later it is more likely stale than useful to an agent that has just
  started. An expired message is no longer offered or counted. Rows go 30
  days after they expire, on the next send anyone makes, through a
  `SECURITY DEFINER` prune. This is the ride-on-the-write pattern presence
  uses. The 30 days keep the sender's "delivered / acknowledged" list
  readable after a message stopped being offered.
- **Counts against the namespace quota** by ciphertext size, through the
  trigger function plans use (ADR 0021).

`agent_message_deliveries` holds one row per (message, credential), written
when the message is handed to that session. `acked_at` is set when the
session acknowledges it. This is what makes a broadcast correct: each
session sees it once, because its delivery row says it already has it. The
message's sender and recipient principal are copied onto the row, so its
policy needs no subquery either.

### Who may do what

- **Sending needs `plan.write` in the project and a person.** The capability
  is the one every role that writes holds (CONTRIBUTOR and up). A message is
  coordination on the shared board, the same kind of act as ticking a step
  or reassigning one, and a new capability would always be set the same as
  `plan.write`. On top of that, **the caller must be a user principal on a
  browser session credential.** That is `requirePerson`, the same check
  idea acceptance uses. An agent reads a message as its user speaking. If a
  token could send, one agent could instruct another in a person's name,
  and a prompt injection in one session could spread to every agent on the
  project. So there is no `inbox.send` tool, and a PAT gets
  `403 needs_person`.
- **Reading is your own inbox only.** Service methods filter by the calling
  credential and principal. RLS says the same thing independently. A member
  sees a message they sent, one addressed to them, and a broadcast. They do
  not see a colleague's direct message, even in a shared project.
  Deliveries are visible to the recipient and to the sender. Nobody can
  insert a message in another's name, because `WITH CHECK
  sender_principal = current principal`. `cc_app` has no UPDATE or DELETE
  on messages.
- **Audited:** `message.send`, naming the message, not its text.
  Deliveries and acks are not audited. Like ticks, they are not security
  events, and the delivery row already records them.

### Delivery: cursor polling, explicit ack

- `GET /v1/me/inbox` and the MCP tool `inbox.next` return the calling
  session's messages that it has not acknowledged. They come oldest first,
  across every project the caller is in, and `?project=` narrows them to one
  project. Pages are keyset pages on `seq`, with an opaque cursor
  (`internal/paging`). The default page is 20 messages, the maximum 100.
- **Reading is delivery.** The read records a delivery row for each message
  returned. The sender sees "delivered" at once. The GET has that side
  effect on purpose: the response is the hand-over, and what it returns does
  not change, because nothing is gone until it is acknowledged.
- **Acknowledgement is explicit.** `POST /v1/me/inbox/ack` and `inbox.ack`
  take message ids. `inbox.next` and the GET also take `ack: true` to
  acknowledge what they return in the same call. A message that was
  delivered but not acknowledged is offered again on the next read without a
  cursor, marked `redelivered`. A client that crashed between reading and
  acting loses nothing. Ack-on-read was rejected as the default for that
  reason, and because "acknowledged" is the one signal that tells the
  sender the agent took the message in. Acking an id that is not the
  session's own changes nothing and is not an error, so ack cannot be used
  to probe for ids.

### The notice on every tool result

`server.AddReceivingMiddleware` (go-sdk, ADR 0016) wraps every `tools/call`.
After the tool has run, and while the session has messages that were never
delivered to it, the middleware appends one text block to the result:

> [Context Commander] You have 2 unread messages from Filip, sent by people
> on the project while you work — call inbox.next to read them.

- **It never changes what the tool returned.** It does not touch
  `structuredContent`, and the tool's own text block stays first. A client
  that reads either one sees exactly what it saw before. The inbox tools get
  no notice, because their result is the messages.
- **It costs one indexed count, at most every five seconds.** The count is
  cached per credential for 5 seconds (`messages.UnreadCacheTTL`). A send
  drops the whole cache, so a new message is noticed on the very next call.
  A read or ack drops that session's entry. The query has a 300 ms budget.
  If it runs out, or fails, the notice is left off and the tool's result
  goes back untouched. The cache is bounded and is dropped whole past
  10,000 sessions.
- **"Unread" means never delivered.** The notice stops as soon as
  `inbox.next` hands the messages over, whether or not they were
  acknowledged. An agent that read a message is not nagged on every call
  until it acks.

### The Claude Code hook

`deploy/mcp-clients/claude-code-inbox-hook.py` is a `PostToolUse` and
`UserPromptSubmit` hook. It reads the same `CC_TOKEN` as the MCP connection,
so it reads that session's inbox. When something is waiting, it prints the
messages as `hookSpecificOutput.additionalContext`, which Claude Code adds to
the model's context straight away, and then acknowledges them. With it, a
message reaches the agent after its next tool call even if that call was to
a tool that is not Context Commander's.

- **It is rate limited.** After a tool call it polls at most once per
  `CC_INBOX_INTERVAL` seconds (default 15). It tracks the interval in a
  per-session state file, which it touches before the request, so parallel
  tool calls make one request between them. A prompt always polls.
- **It fails silent and fast.** The server gets a 2-second timeout. Any
  error prints nothing and exits 0. A server that is down costs one short
  timeout per interval.
- **It acks after printing.** If the ack fails, the next poll offers the
  messages again. A message seen twice is better than one never seen.

Codex CLI has no hook that can add context mid-turn. It gets the notice on
MCP tool results and `inbox.next`.

### How an agent should treat a message

The tool description, the server instructions and docs/AGENT-GUIDE.md say
the same thing. A message is input from a person on the project. Weigh it as
a new instruction from your user in this conversation. The server records
who sent it, and the text cannot change that. Check the inbox when told to,
and at natural points.

### Push to the interface

A send, a delivery and an ack publish to the project's board stream
(`plans.Service.Notify`, the in-process fan-out from ADR 0022). The Plans
tab's "Messages you sent" list then re-reads, and the sender watches
"waiting" turn into "delivered" and then "acknowledged". The dashboard's
presence stream is not told, because it shows no messages.

### Surfaces

- **HTTP:**
  - `POST /v1/projects/{ns}/{slug}/messages` sends, with `body`, `to_client`
    or `to_principal`, and `expires_in_minutes`.
  - `GET /v1/projects/{ns}/{slug}/messages` lists the caller's sent messages
    with their deliveries.
  - `GET /v1/me/inbox` and `POST /v1/me/inbox/ack` are the session's inbox.
- **MCP:** `inbox.next` and `inbox.ack`. There is deliberately no
  `inbox.send`.
- **Interface:**
  - "Message" on every agent session in the Plans tab's "Working now" and
    in the dashboard's "Who is working where".
  - "Message everyone" on the board.
  - "Messages you sent" under the board, with their state.
- **CLI:** none.

## Consequences

- A person can redirect a running agent without waiting for its prompt. The
  message arrives at the agent's next Context Commander call, or after its
  next tool call of any kind with the Claude Code hook.
- Every MCP tool call pays for at most one cached count query, and nothing
  when the count is cached.
- Agents cannot message each other. If that is ever wanted, it needs its own
  decision about how such a message is marked and weighed. It must not
  reuse this channel, which agents are told to treat as their user.
- A token shared by several processes shares one inbox. One token per
  client and machine is now advice for two reasons.
- A sender's direct messages are readable by the sender and the recipient
  principal only. A broadcast is readable by every member. That is what
  "everyone working on the project" means.
