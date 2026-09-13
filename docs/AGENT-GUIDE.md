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
and people work on. It is **not** your memory and not a filesystem. The
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

### 3. Do the work, and write down what outlives it

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

### 4. Leave before you leave

When you stop, one of two things must be true, or the next agent starts blind:

- the work is finished and the handoff is **closed**, or
- there is a **new handoff** saying what is done, what is not, and what comes
  next.

A handoff is explicit continuation state, not a transcript. Write what
someone who never saw your reasoning needs in order to continue. Reference
the artifacts you touched.

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

**Do not invent structure.** No task objects, no conversation lineage you
inferred from timestamps, no relationships you guessed. If the evidence is
not explicit, the relationship is unknown and should stay that way.

**Do not format for humans.** Values come back machine-readable — RFC3339
timestamps, byte counts as integers — because a person's interface formats
them according to that person's preferences. Pass them along as they are.

---

## When something is wrong

| What you see | What it means | What to do |
|---|---|---|
| conflict on write | the artifact moved since you read it | re-read, merge, retry |
| already claimed | another client took the handoff | check `claimed_by_you`; if false, stand down |
| forbidden | your role lacks the capability | report it; do not route around it |
| not found on a skill version | it exists but is not visible at your scope | ask whoever owns it to promote it |
| checksum mismatch | stored content no longer matches what was published | stop; this is a supply-chain event, not a transient error |
| too many requests | you exceeded your credential's rate | back off; you are almost certainly looping |

---

## A short version

Bootstrap. Claim. Work. Write down what outlives you. Close or hand over.
Read the skills. Check `budget.truncated`. Never guess at structure, never
carry a secret, never route around a refusal.
