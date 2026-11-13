# 0026. The idea stack: a durable queue for a planning agent, with claims

## Status

Accepted. Refines the "no task queue of our own" non-goal in `NEXT.md`
(maintainer's decision, 2026-11-13).

## Context

A team of three developers wants somewhere to drop tasks and thoughts —
"ideas" — from the interface or from whatever agent they are working with,
and have a **planning agent** take them one at a time, analyse each in the
project's context and propose a solution on its own. A person then reviews
the proposal.

`NEXT.md` lists "no agent runtime, planner, scheduler or task queue of our
own" as a deliberate non-goal, and `docs/CURRENT-ARCHITECTURE.md` says
Context Commander is not an orchestrator and not a task manager. Handoffs
(migrations/000010, 000018) answer "continue this work", for one
continuation, and plans (ADR 0022) answer "where does the work stand". Neither
is a stack of things nobody has started.

The maintainer decided on 2026-11-13:

- **Context Commander holds the stack. It still runs nothing.** No scheduler,
  no agent runtime, no push to agents. The planning agent is a client like
  any other: something outside the server starts it, it pulls an idea,
  writes a proposal and stops. The non-goal is refined, not dropped — what CC
  will not do is decide when an agent runs or what it runs on.
- Two planning agents must never take the same idea.
- The agent proposes; a person decides.

## Decision

### The model

```text
project
  └─ idea   open → claimed → proposed → accepted | rejected | withdrawn
                   claimed → open        (release, or the lease lapses)
                   proposed → open       (rejected, sent back for a new proposal)
```

One table, `ideas` (migration 000038), project-scoped.

| Field | Class (ADR 0011) |
|---|---|
| title, body, proposal (Markdown), rejection reason | CONTENT PAYLOAD — one encrypted blob per idea, `ResourceType "idea"`, AAD bound to `version` |
| status, priority (low/normal/high, stored 0–2), position, version | OPERATIONAL METADATA |
| created_by/claimed_by principal and client, claimed_at, claim_expires_at, proposed_at, decided_by, decided_at | OPERATIONAL METADATA |
| resulting_plan_id (FK plans, `ON DELETE SET NULL`) | OPERATIONAL METADATA |
| branch, ticket_key, ticket_url | OPERATIONAL METADATA, bounded; `ticket_url` http(s) only, checked in the service and by a `CHECK` |

Everything the queue is ordered and counted by is plaintext, so
`claim_next` and the bootstrap count need no decryption. Idea text counts
against the namespace quota (ADR 0021) through the same trigger function
plans use.

RLS is the identical `cc_has_project_access(...)` policy. `cc_app` gets
`SELECT, INSERT, UPDATE` and **no `DELETE`**: an idea is withdrawn or
rejected, never erased, so "what happened to my idea" always has an answer.

### Claims are leases

- A claim belongs to a **client credential**, as a handoff claim does, so
  two agents of the same person are two claimants.
- It lasts **two hours** (`ideas.ClaimLease`), long enough to read the
  context and write a proposal, short enough that an idea held by a crashed
  agent is back in the queue the same afternoon. `idea.claim` on an idea
  you hold renews it. There is no separate MCP renew tool: one name fewer
  for a model to choose between, and "claim it again" is what a model tries
  anyway.
- A lapsed claim is not cleared by a sweeper — there is nothing running to
  sweep. It stays `claimed` with its holder recorded, reads as
  `claim_lapsed`, and the next claim takes it. Its old holder may still
  renew or propose until someone else does; after that it is told
  `not_claimant`.

### claim_next

`ClaimNext` takes the head of the queue: open ideas plus lapsed claims,
ordered by priority (high first), then the team's manual position, then age.

```sql
SELECT id FROM ideas
WHERE project_id = $1 AND (status = 'open' OR (status = 'claimed' AND claim_expires_at <= $now))
ORDER BY priority DESC, position, created_at, id
LIMIT 1 FOR UPDATE SKIP LOCKED
```

`SKIP LOCKED` makes concurrent callers take different ideas instead of
queueing on one row; a row another caller committed meanwhile is re-checked
under its lock and dropped. `TestConcurrentClaimNextNeverHandsOutAnIdeaTwice`
runs twice as many claimers as ideas at once and checks each idea has
exactly one claimant; it fails without `FOR UPDATE SKIP LOCKED`.

A client that already holds a live claim gets **that** idea back
(`resumed: true`) rather than a second one, so a retried call after a lost
reply does not take two — the same concern `claimed_by_you` answers for
handoffs.

An empty queue is an answer (`claimed: false`, HTTP 204), not an error. The
agent is told to stop, not to poll.

### Proposals and draft plans

`Propose` attaches the proposal and, optionally, creates a **draft plan**
through `internal/plans`, linked as `resulting_plan_id`, in the idea's own
transaction (`plans.CreatePlanInTx`). A proposal pointing at a plan that was
never written, or a draft plan no idea knows of, are both states nobody
could clean up by looking. A new proposal replaces the old one and clears
the rejection reason it answered.

- **Accept** moves the draft plan to `active` — the moment the work is
  decided on.
- **Reject** abandons the draft plan and stores the reason. With `reopen`
  the idea goes back to the queue in its old place, and the next agent reads
  the rejected proposal and the reason before proposing; without it the idea
  is rejected for good.
- **Withdraw** (any writer, interface and HTTP only) takes an undecided idea
  off the stack and abandons its draft plan.

A decision only moves a plan that is still `draft`
(`plans.SetStatusInTx`): a person who already started or finished the plan
by hand is not overruled.

### Who may do what

Two capabilities, same tiers as plans: `idea.read` for every role,
`idea.write` for every role that can write. A reader reads; contributors,
admins and owners add, claim, propose, reorder and withdraw.
`AuthzService.Can()` is called at the top of every service method
(ADR 0014).

**Accepting and rejecting need a person, not a capability.** The decision
is the one control that keeps an autonomous agent from approving its own
work, and a role cannot express it: an agent on its user's PAT holds exactly
the user's role, and a ServicePrincipal could be granted any role. What
does tell a person's click from an agent's call is the credential:

- a browser session is issued only by an interactive sign-in (password,
  LDAP, OIDC or a pasted token exchanged in the browser, ADR 0019), lives in
  an HttpOnly cookie no script can read, and every request on it carries the
  CSRF header;
- a PAT is exactly what agents hold.

So `Accept` and `Reject` require a **user principal on a `session`
credential** (`ideas.ErrNeedsPerson`, HTTP 403 `needs_person`). A dedicated
`idea.decide` capability was considered and rejected: it would be granted to
the same roles as `idea.write` and stop nothing, because the agent acting
for a user has the user's capabilities. MCP has no accept or reject tool,
and the inventory test says so. Releasing someone else's claim is allowed
on the same terms, so a team can free an idea held by an agent they know is
gone without waiting out the lease.

The person check is not a sandbox: someone who pastes their own session
cookie into an agent has handed it their hands. It is a line agents do not
cross by accident or by instruction-following, which is what was asked.

### Order

Within one priority, open ideas carry a manual position. A move names a
1-based place within that priority's line and renumbers the line densely
under a per-project advisory lock (`db.LockClassIdeaOrder`), as phases do
(ADR 0022, "Reordering"); a new priority sends the idea to the end of that
line. Moves are not audited, like plan ticks.

`List` pages open and claimed ideas in queue order with a rank cursor
(priority, position, created_at, id) and everything else most recently
changed first with a time cursor; a cursor from one ordering is refused by
the other. Lists carry titles only; `Get` carries the content.

### Audit, events, push

Create, claim, release, propose, accept, reject and withdraw write an audit
row in the mutation's transaction. Renewals and reorders do not. No outbox
events are emitted: nothing consumes them, and the emitted event set
(`docs/CURRENT-ARCHITECTURE.md` §4.5) is not extended speculatively. The
Ideas screen is told of changes over the **plan board's stream** (ADR 0022,
"Push instead of polling") — an idea change publishes there too — so a
project screen holds one stream, not two, against the four a credential may
open.

### Surfaces

- **MCP:** `idea.add`, `idea.list`, `idea.get`, `idea.claim_next`,
  `idea.claim` (also renews), `idea.release`, `idea.propose`. The planning
  tools' descriptions spell out the loop: claim_next → read context
  (`context.bootstrap`, `plan.board`) → analyse → propose with a draft plan
  → stop, never implement.
- **HTTP:** `/v1/projects/{ns}/{slug}/ideas` — `POST /`, `GET /?status=…`,
  `POST /claim-next`, `GET|PATCH /{id}`, and `POST /{id}/claim|renew|release|
  propose|accept|reject|withdraw`.
- **`context.bootstrap`** carries `ideas: {open, claimed, proposed,
  claimable}` when anything waits — four integers, charged nothing.
- **Interface:** an Ideas tab per project — quick add, the proposals waiting
  for a decision, the queue with move and priority controls, what is being
  analysed, and what was decided. The proposal is shown as preformatted
  text: the app has no Markdown renderer and this change does not add an
  HTML one for model output.
- **A ready prompt** for a planning agent:
  `deploy/mcp-clients/planning-agent.md`.

## Consequences

- A team gets a stack it can fill from anywhere and an agent can drain one
  idea at a time, with no idea taken twice and none held forever.
- Context Commander still starts no process. Running the planning agent —
  a cron job, a CI schedule, a person typing "next idea" — is the operator's
  choice and outside this server, which is what keeps the non-goal true.
- The two-hour lease is a constant, not a setting. If real analyses run
  longer, the agent renews; if that proves common, the lease is the thing
  to revisit, not a sweeper.
