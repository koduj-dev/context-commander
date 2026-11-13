# 0025. Memories: what agents learned, with the branch and ticket it belongs to

## Status

Accepted, 2026-11-13.

Revises the "not memory for AI" stance in NEXT.md ("Positioning", the
strategy table and "Deliberately not doing") for this one primitive. It does
not revise the rest of that stance: there is still no vector database, no
embedding search and no general knowledge base.

## Context

A team of three developers, each running coding agents on the same project,
wants those agents to keep writing down what they learn as they work —
decisions, the cause of a bug, a trap that looks right and is not, where a
piece of work stands — and to search for it before starting the next task.
The value is in the metadata as much as the text. "Everything we found on
`feature/billing`", "what do we know about PROJ-123" and "what did Alice's
agent decide" are the questions they ask.

Nothing in the system answers them today:

- **Artifacts** are files at a path the writer chooses. Nobody can filter
  them by ticket or branch, and an agent that wants to record one finding
  has to pick a file, read it, merge and write it back.
- **Handoffs** are continuation state for the next agent on the same work,
  closed when that work is picked up.
- **Plans** (ADR 0022) are what is about to be done. A finding outlives the
  plan it came out of.

NEXT.md had ruled out "memory for AI" because a general memory store makes
relevance the product. The maintainer decided on 2026-11-13 to build this
one primitive anyway: project-scoped, written deliberately by agents and
people, filtered by explicit metadata, and searched by text, not by
similarity.

## Decision

### The model

One table, `memories` (migration 000037), scoped to a project like every
other content table. A memory has:

| Field | Class (ADR 0011) | Notes |
|---|---|---|
| title, body | CONTENT PAYLOAD | One encrypted blob per row (`ResourceType "memory"`, AAD version = `version`). Body is markdown, at most 32 KiB; title at most 300 characters. |
| `kind` | operational metadata | `decision`, `finding`, `gotcha`, `note`, `status` or `other` (CHECK constraint). |
| `status` | operational metadata | `active`, `superseded` or `archived`. |
| `branch` | operational metadata, **allowlisted here** | The git branch, at most 255 characters, no whitespace or control characters. |
| `ticket_key` | operational metadata, **allowlisted here** | For example `PROJ-123`, at most 64 characters, no whitespace. |
| `ticket_url` | operational metadata, **allowlisted here** | An absolute `http` or `https` URL with a host and no credentials, at most 2048 characters. Checked in Go and by a CHECK constraint. |
| `tags` | operational metadata, **allowlisted here** | At most 20, each at most 50 characters, trimmed, lowercased and de-duplicated. |
| `assignee_principal` | operational metadata | Who solved or owns the thing the memory is about. Must be a project member. |
| `plan_id`, `plan_item_id` | operational metadata | Optional links to the plan and step the memory came out of. Both must be in the same project, and the step must belong to the plan. `ON DELETE SET NULL`, so deleting a plan unlinks its memories but keeps them. |
| `superseded_by` | operational metadata | The memory in the same project that replaced this one. `ON DELETE SET NULL`. |
| `created_by_principal`, `created_by_client`, `version`, timestamps | operational metadata | As on plans. |

**Why branch, ticket and tags are plaintext, and the risk this accepts.**
Filtering by them is the whole point. If they were encrypted, every filter
would mean decrypting the project's memories, and that bound (below) would
apply to "what do we know about PROJ-123", the question asked most. Keeping
them plaintext means anyone who can read the database — an operator, a
backup, someone who steals a dump — can read the names of branches, the
ticket keys and URLs, and the tags. They will learn what is being worked on
and what it is called. A branch called `fix/acme-breach-cve-2026-1234` says
a lot on its own. This is accepted because the same reader can already see
project and namespace names, artifact paths and plan statuses, and because
the rule for writers is simple: **the text goes in the title and body; the
labels are labels.** The tool descriptions and docs/AGENT-GUIDE.md say so.
A URL with credentials in it is refused, because putting a password in the
plaintext metadata by accident is the one mistake here that would leak a
secret.

### Bounded growth

The same rules as plans:

- **No history of the text.** An edit rewrites the blob and bumps `version`.
- **An edit needs the version the writer read** (`expected_version`). That
  covers metadata too: an edit is one memory, and a writer working from a
  stale read should be told. A metadata-only edit re-seals the blob anyway,
  because the version is part of the ciphertext's AAD.
- **Changing status needs no version.** Archiving, bringing back and
  superseding rewrite no text, and doing one twice is the same as doing it
  once.
- **Create and delete are audited** (`memory.create`, `memory.delete`).
  Edits and status changes are not, and no outbox event is written: plans
  write none for their edits, and the row's `updated_at` says when something
  changed.
- **Delete is a real delete**, as for plans. The row and its ciphertext go,
  and the bytes go back to the namespace quota (ADR 0021, through the same
  trigger function plans use). Archiving is the way to keep a memory without
  it counting as current.

### Who may do what

Memories reuse the plan capabilities: every role has `plan.read` and may
read, and every role that can write artifacts has `plan.write` and may
write. They are the same kind of shared working record, and a team member
who can tick a step can say what they learned doing it. A separate pair of
capabilities would split one decision into two settings that would always be
set the same way. `memories.Service` checks the capability at the top of
every method (ADR 0014). RLS uses the same `cc_has_project_access` policy as
every project table. `cc_app` gets SELECT, INSERT, UPDATE and DELETE. A
negative test as `cc_app` shows that one project's memories cannot be seen
from another.

### Listing and search

- **List** pages newest first with the keyset cursor from `internal/paging`.
  It filters in SQL by kind, branch, ticket key, tag, assignee, author,
  status and creation time. With no status filter it returns active
  memories only: handing an agent a superseded decision as if it still held
  is worse than handing it nothing. `status=any` returns all of them.
- **Search** matches text, which is encrypted, so it decrypts. It applies
  the metadata filters in SQL first, decrypts at most the newest **1000**
  memories that pass, and matches the query case-insensitively in Go.
  Title matches rank before body matches, newest first within each. It
  returns at most **100** hits, each with a snippet instead of the body.
  `truncated` says when either limit cut the answer, so a caller can tell
  "nothing more" from "narrow it down". Adding a filter shrinks what has to
  be decrypted, which is why the filters exist.
- **Search across projects** (`memory.search` without a project, and
  `GET /v1/me/memories/search`) works like plan search across projects. It
  covers every project where the caller's role grants `plan.read`, at most
  50 projects (the caller's newest), and at most 200 candidates per project.
  Each project is decrypted with its own key, and everything is read under
  the caller's RLS context.
- **The seam for semantic search** is `memories.Ranker`. It takes the query
  and the candidates, already narrowed by the metadata filters, already
  authorized and decrypted, and returns ranked hits. `TextRanker` is the
  substring match. A later embedding ranker replaces it through
  `Service.SetRanker`, and no caller changes. Embeddings are **not** built
  here. Where they would be stored, and whether a vector of encrypted text
  is itself content that must be encrypted, are that later change's
  questions.

### Surfaces

- **MCP:**
  - `memory.create`, `memory.get`, `memory.update`, `memory.list`,
    `memory.search`, and `memory.supersede`. `memory.supersede` also
    archives and brings memories back.
  - The descriptions tell an agent **when** to write a memory (a decision, a
    cause found, a gotcha, a status worth handing on; not routine progress)
    and to fill in branch and ticket whenever it knows them. They also tell
    it to search before starting a task.
  - There is no `memory.delete` over MCP, just as there is no `plan.delete`.
    Retiring a memory is supersede or archive.
- **HTTP:**
  - `/v1/projects/{ns}/{slug}/memories`: list, create, `search`, `facets`,
    `{id}` (get, patch, delete) and `{id}/status`.
  - `/v1/me/memories/search` searches across projects.
  - `facets` returns the branches, tickets, tags and assignees that active
    memories use, most used first, so the interface offers real values as
    filters. It is plaintext metadata only, and nothing is decrypted to
    answer it.
- **CLI:** none. Plans have no CLI either.
- **`context.bootstrap`:** a `memories` section lists the 10 newest active
  memories with titles and metadata, never bodies, and says when there are
  more. Unlike the handoff and the work summary, it is left out when it does
  not fit the budget. It points to knowledge the agent can search for, not
  the assignment, so leaving it out costs little. When it is left out, a
  truncation note names `memory.list` and `memory.search`.
- **Interface:**
  - A **Memories** tab per project. Filters for kind, branch, ticket, tag,
    assignee and status, with active filters shown as removable chips, and a
    search box that combines with them.
  - A detail view shows the metadata. The ticket is an external link with
    `rel="noopener noreferrer"`, rendered only for `http` and `https` URLs,
    and checked again in the browser.
  - People can write a memory from the tab, archive one, bring it back, or
    delete it.

## Consequences

- An agent can leave behind what it learned in one call, and the next one,
  on any client, finds it by ticket, branch or words. A project that never
  writes a memory sees nothing new except an empty tab.
- Branch names, ticket keys and URLs, and tags are readable by anyone who
  can read the database. Writers must keep sensitive detail in the title and
  body.
- Search pays for decryption. One project's search decrypts at most 1000
  memories, and a project with more relies on filters to narrow it down. If
  real use shows that this is not enough, the Ranker seam is where an index
  goes, and the limit says when it is needed.
- "Not memory for AI" stays true where it mattered. There is no automatic
  capture, no similarity search, and no store outside a project. Each memory
  is something an agent or person chose to write, about one project, with
  provenance.
