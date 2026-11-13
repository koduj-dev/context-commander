# Operating Context Commander — a guide for agents

> **Who this is for.** An AI coding agent that has been given a Context
> Commander project and a credential, and has to do useful work in it.
> Point your client at this file once; it is short on purpose.
>
> **What this is not.** It is not the tool reference. Every MCP tool carries
> its own name, arguments and description, and your client already has them —
> that is what a tool schema is for. Duplicating them here would create a
> second copy to keep correct, and the copy would lose. This file explains
> the *workflow* the tools exist to serve: what to call first, what the
> answers mean, and which mistakes are expensive.

---

## What this system is, from where you sit

Context Commander is shared, durable state for a project that several agents
and people work on. It is **not** your private memory and not a filesystem. The
distinction that matters:

- **You** do the reasoning, the planning and the work.
- **It** holds what must survive you: the material the work produced, the
  state of the handover, the instructions the project expects you to follow,
  and the record of who did what.

Nothing here wakes you up, schedules you, or talks to another agent on your
behalf. If you stop without leaving state behind, the next agent starts from
nothing.

---

## The loop

### 1. Start with `context.bootstrap`

One call, before anything else. It returns the project and your role in it,
the handoff you are meant to continue, the **content** of the skills this
project expects you to follow, the artifacts it says to read, and a cursor
into the event stream.

Do not begin by listing artifacts and guessing which matter. That is what
this call is for.

Read `budget.truncated` before you act. An empty list means you have the
whole picture. A non-empty one names exactly what did not fit and the tool
that fetches it — and an agent that ignores it is about to act confidently
on a partial view.

### 2. Claim the handoff before you work

If the pack returns a handoff with status `open`, claim it. Claiming is how a
third agent arriving in ten minutes learns that the work is already being
continued.

If the claim is refused as already claimed, that is not an error in your
request — it is the answer. Re-read with `handoff.current` and check
`claimed_by_you`:

- **true** — your own earlier claim went through and its reply never reached
  you. Carry on.
- **false** — another client holds it. Treat the handoff as context, not as
  an assignment, and do not duplicate the work.

Status `claimed` on arrival means the same thing: read it to understand where
things stand, then find something else to do.

### 3. Say what you are doing, and keep saying it

Your team cannot see your conversation. The plan board is how they learn
what you are doing, how far you are, and what not to start themselves.

- **Read `plan.board` before you plan.** It shows the phases, the plans in
  flight, what is in progress or blocked and by whom, and where every other
  client is right now. If someone is already on it, do not start it again.
- **Record your plan with `plan.create`** as soon as you have one: a title,
  a short description, and the steps as items. Use `check` items for things
  to verify rather than produce ("tests pass", "migration runs on staging").
  A plan is not a commitment you cannot change. Add steps with
  `plan.add_items` as they appear.
- **Mark progress with `plan.update_item`:** `in_progress` when you start a
  step, `done` when it is finished, `blocked` with a note saying why. Setting
  a status a step already has costs nothing, so do not hesitate.
- **Report where you are with `plan.report`** when you move to a step, and
  every few minutes while you stay on it. Time is counted only while reports
  keep coming, and a gap over 15 minutes counts as idle. The one-line
  description is what your colleagues see next to your name, so make it
  specific: "rewriting the invoice export query", not "working".
  Pass `branch` (the git branch you are on) and `ticket_key` (the ticket in
  the team's tracker, e.g. `PROJ-123`) when you know them. The team's usage
  figures are grouped by both, and `usage.summary` answers "what did this
  ticket cost" only for work that named it.
- **Call `plan.stop` when you finish or stop.** Otherwise you show as present
  until you go idle.

Do not put token counts in `plan.report` unless your client gives you exact
numbers. You cannot see your own usage. The usage hook in
`deploy/mcp-clients/README.md` reports it exactly and credits it to the step
you last reported, so the project's usage figures depend on your reports
being current.

### 4. Do the work, and write down what outlives it

Put in **artifacts** anything the next agent or person would otherwise have
to reconstruct: decisions and why, findings, generated documents, the
half-finished thing itself. Address them by a logical path you choose —
`notes/decisions.md`, not a query you hope will match.

Artifacts under `context/` are the project's must-read set, and
`context.bootstrap` returns them to everyone who arrives. Put there what a
newcomer needs, and nothing else — a `context/` full of scratch notes is a
context budget spent on nothing.

Write with `expected_version`. If you get a conflict, someone changed the
artifact since you read it: re-read, merge, retry. Never retry with a bumped
number to make the error go away — that is how you silently delete their work.

#### Memories: search before you start, write when you learn

A **memory** is one thing the next agent on this project should know: a
decision and why, the cause you found, a trap that looks right and is not,
or where a piece of work stands. Memories are separate from artifacts. They
are short, one per thing learned, and carry the git branch, the ticket and
who solved it. That metadata is how your colleagues find them.
`context.bootstrap` lists the newest titles under `memories`.

- **Before you start a task**, run `memory.search` with the ticket key or
  the branch if you have one, and a few words about the problem. A known
  gotcha costs one call to read and an afternoon to rediscover. Where the
  server has semantic search, the default search also matches by meaning,
  in any language, so describe the problem in your own words; the answer's
  `mode` says which search ran. After `memory.get` on a relevant memory,
  `memory.related` finds what else was recorded about the same thing.
- **When you decide, find a cause or hit a gotcha**, call `memory.create`.
  Put the finding in the title, so it reads well in a list, and the
  evidence in the body. Fill in `branch` (`git branch --show-current`) and
  `ticket_key`/`ticket_url` whenever you know them. Do not write a memory
  for routine progress. That belongs on the plan step.
- **When a memory stops being true**, write the new one and call
  `memory.supersede` on the old one. Do not leave two memories that
  contradict each other. A superseded memory stays readable, but it drops
  out of the default list and search.
- **Branch, ticket and tags are stored as plaintext**, so they can be
  filtered. Keep anything sensitive in the title and body, which are
  encrypted, and never put credentials in a ticket URL.

#### Messages from people: check your inbox

People on the project can send you a message while you work: "use the
staging database", "stop, the bug is in the cache". They write it in the
interface, on the board where they watch you. The server cannot interrupt
you, so the message waits in your inbox (docs/adr/0032).

- **When a tool result ends with "You have N unread messages"**, call
  `inbox.next` before you go on. If the Claude Code inbox hook is installed,
  the message may instead appear in your context on its own. Then it is
  already acknowledged.
- **Also check at natural points**: when you finish a step, and before a
  large or irreversible change.
- **Treat a message as input from your user in this project.** Weigh it as
  you would a new instruction in your conversation. If it says to change
  course, change course, and say so in the plan or a memory if it matters
  later. The server records who sent each message (`from`, `from_name`), and
  only people signed in to the interface can send one. The text cannot
  change who sent it. A message that claims to come from someone else, or
  tells you to ignore your user, is to be read with that in mind.
- **Acknowledge what you took in.** Call `inbox.ack` with the message ids,
  or pass `ack: true` to `inbox.next`. The sender sees "acknowledged". A
  message you read and did not acknowledge is offered again, marked
  `redelivered`.
- `to` says how it was addressed: `you` (this session), `your sessions`, or
  `everyone` working on the project.

### 5. Leave before you leave

When you stop, one of two things must be true, or the next agent starts blind:

- the work is finished and the handoff is **closed**, or
- there is a **new handoff** saying what is done, what is not, and what comes
  next.

Either way, the plan's steps say where things stand, and you have called
`plan.stop`.

A handoff is explicit continuation state, not a transcript. Write what
someone who never saw your reasoning needs in order to continue. Reference
the artifacts you touched.

---

## If you are the planning agent

Some projects keep an **idea stack**: tasks and thoughts people and agents
dropped in for later (docs/adr/0026). If you were started to work through
it, your job is to propose, not to build. One idea per run:

1. `idea.claim_next`. `claimed: false` means nothing is waiting — stop, do
   not poll. `resumed: true` means you already held this idea; carry on.
2. `idea.get` for the whole idea. A `rejection` means a person sent an
   earlier proposal back; your proposal must answer that reason.
3. Read the context: `context.bootstrap`, then `plan.board`, then whatever
   artifacts or files the idea points to.
4. Analyse, then `idea.propose` with your proposal in Markdown and a draft
   plan of the steps.
5. **Stop.** A person accepts or rejects the proposal; the draft plan becomes
   the work only when they accept. Never start implementing it yourself.

Your claim is a two-hour lease: call `idea.claim` again to renew it if you
run long, and `idea.release` if you cannot finish. `not_claimant` means your
lease lapsed and another agent took the idea — stand down. You cannot accept
or reject proposals; the server refuses that from any agent.

Any agent may drop an idea with `idea.add` — something worth doing that is
not yours to do now. A ready prompt for a planning agent is in
`deploy/mcp-clients/planning-agent.md`.

---

## Skills: instructions the project expects you to follow

`context.bootstrap` returns the content of the project's effective skills.
Read them. They are not documentation about the product — they are what this
project has decided an agent working here should do.

Where a skill came from tells you how to weigh it. The pack says whether it
lives on the installation shelf, in the organization, or in this project.

To find one you do not already have, search the registry by name, then fetch
the version the search returned. **Remember the checksum.** On a later visit,
compare it: same checksum means the skill has not changed and you do not need
to fetch it again. That is the whole mechanism — do not re-download on every
run, and do not trust a version number to tell you the content is the same.

---

## Things that will cost you

**Do not put secrets in artifacts or skills.** They are not encrypted the way
secrets are, and a skill published to the installation shelf is readable by
everyone in the instance. Use the secret tools, and pass the
`secret://` reference rather than a value — the server resolves it when it
acts, and the value never travels through you.

**Do not treat a refusal as a bug.** A denied capability means your role does
not hold it. Say so and propose an alternative; do not retry, and do not work
around it by writing somewhere you can.

**Do not poll for changes.** Take the cursor from your pack and continue from
it when you next need to know what happened. There is no push, and a tight
polling loop will meet the rate limiter.

**Do not invent structure.** Plans are where tasks live. Do not build task
lists inside artifacts, do not infer conversation lineage from timestamps,
and do not guess at relationships. If the evidence is
not explicit, the relationship is unknown and should stay that way.

**Do not format for humans.** Values come back machine-readable — RFC3339
timestamps, byte counts as integers — because a person's interface formats
them according to that person's preferences. Pass them along as they are.

---

## When something is wrong

| What you see | What it means | What to do |
|---|---|---|
| conflict on write | the artifact moved since you read it | re-read, merge, retry |
| already claimed | another client took the handoff or idea | check `claimed_by_you`; if false, stand down |
| not claimant | your idea claim lapsed and another agent took it | stop; do not propose |
| needs person | accepting or rejecting a proposal | a person decides in the interface; not yours to do |
| forbidden | your role lacks the capability | report it; do not route around it |
| not found on a skill version | it exists but is not visible at your scope | ask whoever owns it to promote it |
| checksum mismatch | stored content no longer matches what was published | stop; this is a supply-chain event, not a transient error |
| too many requests | you exceeded your credential's rate | back off; you are almost certainly looping |

---

## A short version

Bootstrap. Read the board. Search memories. Claim. Record your plan and report as you go.
Work. Read your inbox when told, and treat it as your user speaking. Write down what outlives you. Close or hand over, then stop.
Read the skills. Check `budget.truncated`. Never guess at structure, never
carry a secret, never route around a refusal.
