-- Memories — docs/adr/0025-memories.md.
--
-- What an agent or a person learned about a project and wants the next one
-- to know: a decision, a finding, a gotcha, a status note. The title and the
-- body are CONTENT PAYLOAD (docs/adr/0011) and live encrypted on the row.
-- Everything a team filters by — kind, branch, ticket, assignee, author,
-- tags, the plan it came from, status — is OPERATIONAL METADATA and stays
-- plaintext on purpose; ADR 0025 allowlists each column and states the risk.
--
-- Growth is bounded like plans': an edit rewrites the blob and bumps the
-- version, and no history of the text is kept.

CREATE TABLE memories (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  kind                 text NOT NULL DEFAULT 'note'
                         CHECK (kind IN ('decision', 'finding', 'gotcha', 'note', 'status', 'other')),
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'archived')),
  -- Bumped on every rewrite of the blob; also the AAD's ResourceVersion.
  version              int NOT NULL DEFAULT 1,
  -- Plaintext metadata. Bounded here as well as in internal/memories, so a
  -- second writer cannot store what the service would refuse.
  branch               text CHECK (length(branch) BETWEEN 1 AND 255),
  ticket_key           text CHECK (length(ticket_key) BETWEEN 1 AND 64),
  ticket_url           text CHECK (length(ticket_url) <= 2048 AND ticket_url ~ '^https?://'),
  tags                 text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 20),
  assignee_principal   uuid REFERENCES principals (id),
  plan_id              uuid REFERENCES plans (id) ON DELETE SET NULL,
  plan_item_id         uuid REFERENCES plan_items (id) ON DELETE SET NULL,
  superseded_by        uuid REFERENCES memories (id) ON DELETE SET NULL,
  ciphertext           bytea NOT NULL,
  nonce                bytea NOT NULL,
  wrapped_dek          bytea NOT NULL,
  project_key_version  int NOT NULL,
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_by_client    uuid REFERENCES client_credentials (id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);
-- The list and the search walk newest first, filtered by project and
-- status; the narrower filters are what a team clicks on.
CREATE INDEX memories_project_idx ON memories (project_id, created_at DESC, id DESC);
CREATE INDEX memories_project_status_idx ON memories (project_id, status, created_at DESC);
CREATE INDEX memories_branch_idx ON memories (project_id, branch) WHERE branch IS NOT NULL;
CREATE INDEX memories_ticket_idx ON memories (project_id, ticket_key) WHERE ticket_key IS NOT NULL;
CREATE INDEX memories_assignee_idx ON memories (project_id, assignee_principal) WHERE assignee_principal IS NOT NULL;
CREATE INDEX memories_tags_idx ON memories USING gin (tags);

ALTER TABLE memories ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON memories
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- DELETE because a memory can be deleted outright, like a plan: it gives
-- its bytes back to the quota and leaves nothing behind to decrypt.
GRANT SELECT, INSERT, UPDATE, DELETE ON memories TO cc_app;

-- The text counts against the namespace quota (docs/adr/0021) by the size
-- of its ciphertext, through the same function plans use.
CREATE TRIGGER memories_storage
  AFTER INSERT OR DELETE OR UPDATE OF ciphertext ON memories
  FOR EACH ROW EXECUTE FUNCTION cc_plan_text_storage();
