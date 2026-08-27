# 0011. Encryption invariant covers all content-bearing payloads, not just artifacts

## Status

Accepted

## Context

Round 1 stated "Artifacts are always encrypted at rest." Round 2 (blocker D) pointed out this is
too narrow: the original security requirement in `docs/architecture-cookbook.md` §14 was motivated
by *any* sensitive content landing on a shared server — chat history, compacted history, handoff
content, prompts, summaries, tokens — not artifacts specifically. `internal/history`'s
`HistoryObject` (raw/compacted/summary/imported) and `internal/handoffs`' `Handoff` body were left
unclassified.

## Decision

Every subsystem that stores content classifies each field into exactly one of:

| Class | Rule | Examples |
|---|---|---|
| **INDEXABLE / OPERATIONAL METADATA** | may stay plaintext, only if explicitly allowlisted | artifact `content_type`, handoff `status`/`id`/timestamps, `HistoryObject.type`/`provider`/`created_at` |
| **CONTENT PAYLOAD** | always encrypted via `CryptoProvider`, same mechanism as artifacts (incl. AAD, [0005](0005-key-hierarchy-and-artifact-size-limit.md)) | artifact content, handoff body (summary/next steps/context), `HistoryObject` body (raw/compacted/summary/imported text) |
| **SECRET** | never stored as ordinary content payload, only via a `secret://` reference | tokens, passwords, Git credentials, API keys |

`domain_events` ([0013](0013-transactional-outbox-and-storage-lifecycle.md)) additionally carries
`payload_kind` (`metadata_only` | `encrypted_payload_ref`) so a feed consumer knows up front
whether the event body itself is safe to read or is only a pointer to something requiring
authorized fetch.

## Consequences

- `internal/handoffs.Handoff.Body` and `internal/history.HistoryObject.Body` go through
  `CryptoProvider.Encrypt` with `ResourceType` set to `"handoff"` / `"history_object"`
  respectively — same code path as artifacts, not a bespoke encryption call per subsystem.
- Adding a new content-bearing field to any subsystem requires classifying it against this table in
  its own ADR or PR description before it ships, not after.
- `audit_log`'s allowlisted-columns rule (§ below) is this same classification applied to the audit
  subsystem: `metadata` is never a free `jsonb` dump; a sensitive audit payload goes into
  `encrypted_audit_payload` as CONTENT PAYLOAD.
