-- Conversation / ExternalSession / HistoryObject lineage —
-- docs/architecture-cookbook.md §10. Provider session IDs are opaque
-- metadata, never canonical identity (§10.1): Context Commander owns its
-- own stable Conversation identity, and ExternalSession records each
-- provider-side session as an edge into it. Compaction never overwrites
-- original history (§10.3) — history_objects is append-only: cc_app has no
-- UPDATE/DELETE grant on it at all, so that invariant holds at the database
-- layer, not just by code-review discipline.
--
-- project_id is denormalized onto all three tables (same reasoning as
-- artifact_versions, migrations/000003_artifacts.up.sql) so each can carry
-- its own RLS policy without joining back to conversations every time.
CREATE TABLE conversations (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX conversations_project_id_idx ON conversations (project_id);

ALTER TABLE conversations ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON conversations
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

GRANT SELECT, INSERT ON conversations TO cc_app;

-- relation widens cookbook §10.2's narrower list (started/continued/
-- compacted_from/forked_from) to match §10.4's fuller relation vocabulary
-- (adds imported_from) — the two subsections list slightly different sets;
-- this reconciles them rather than picking one arbitrarily.
CREATE TABLE external_sessions (
  id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id                 uuid NOT NULL REFERENCES projects (id),
  conversation_id            uuid NOT NULL REFERENCES conversations (id),
  provider                   text NOT NULL,
  client                     text NOT NULL,
  provider_session_id        text NOT NULL,
  parent_external_session_id uuid REFERENCES external_sessions (id),
  relation                   text NOT NULL CHECK (relation IN ('started', 'continued', 'compacted_from', 'forked_from', 'imported_from')),
  started_at                 timestamptz NOT NULL,
  ended_at                   timestamptz
);

CREATE INDEX external_sessions_project_id_idx ON external_sessions (project_id);
CREATE INDEX external_sessions_conversation_id_idx ON external_sessions (conversation_id);

ALTER TABLE external_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON external_sessions
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

GRANT SELECT, INSERT ON external_sessions TO cc_app;

-- Body is CONTENT PAYLOAD (docs/adr/0011) — encrypted the same envelope way
-- as handoffs (migrations/000010_handoffs.up.sql), ResourceType =
-- "history_object". derived_from records the compaction/summarization edge
-- between two HistoryObjects (§10.3's "raw_A --compacted_to--> compact_B");
-- it is a self-reference, never an in-place edit of the original row.
CREATE TABLE history_objects (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id          uuid NOT NULL REFERENCES projects (id),
  conversation_id     uuid NOT NULL REFERENCES conversations (id),
  external_session_id uuid NOT NULL REFERENCES external_sessions (id),
  type                text NOT NULL CHECK (type IN ('raw', 'compacted', 'summary', 'imported')),
  derived_from        uuid REFERENCES history_objects (id),
  content_hash        text NOT NULL,
  ciphertext          bytea NOT NULL,
  nonce               bytea NOT NULL,
  wrapped_dek         bytea NOT NULL,
  project_key_version int NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);

CREATE INDEX history_objects_project_id_idx ON history_objects (project_id);
CREATE INDEX history_objects_conversation_id_idx ON history_objects (conversation_id);

ALTER TABLE history_objects ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON history_objects
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- Append-only: no UPDATE, no DELETE. This is how "compaction never
-- overwrites the original" (§10.3) is enforced — not just by there being no
-- Update method in internal/history.Service, but by cc_app being
-- structurally unable to run one even if a future code path tried.
GRANT SELECT, INSERT ON history_objects TO cc_app;
