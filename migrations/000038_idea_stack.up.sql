-- The idea stack — docs/adr/0026-idea-stack.md.
--
-- People and agents drop ideas here; a planning agent outside this server
-- takes them one at a time, analyses them and proposes a solution, which a
-- person accepts or rejects. Context Commander runs nothing: it is the
-- durable stack an agent pulls from, with claims so two agents never take
-- the same idea.
--
-- Title, body, proposal and rejection reason are CONTENT PAYLOAD
-- (docs/adr/0011) and live in one encrypted blob on the row. Status,
-- priority, position, who claimed it and until when, the resulting plan and
-- the branch/ticket references are OPERATIONAL METADATA and stay plaintext,
-- so the queue can be ordered and counted without decrypting anything.

CREATE TABLE ideas (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  status               text NOT NULL DEFAULT 'open'
                       CHECK (status IN ('open', 'claimed', 'proposed', 'accepted', 'rejected', 'withdrawn')),
  -- 0 low, 1 normal, 2 high. Three levels a person can tell apart, not a
  -- number two people would pick differently for the same idea.
  priority             smallint NOT NULL DEFAULT 1 CHECK (priority BETWEEN 0 AND 2),
  -- Manual order among open ideas of the same priority (internal/ideas).
  position             int NOT NULL,
  -- Bumped on every rewrite of the blob; also the AAD's ResourceVersion.
  version              int NOT NULL DEFAULT 1,
  ciphertext           bytea NOT NULL,
  nonce                bytea NOT NULL,
  wrapped_dek          bytea NOT NULL,
  project_key_version  int NOT NULL,
  -- Where the work lives, bounded as memories bound the same fields
  -- (migrations/000037). The URL is http(s) only, so the interface never
  -- renders a javascript: link.
  branch               text CHECK (length(branch) <= 255),
  ticket_key           text CHECK (length(ticket_key) <= 64),
  ticket_url           text CHECK (length(ticket_url) <= 2048 AND ticket_url ~* '^https?://'),
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_by_client    uuid REFERENCES client_credentials (id),
  -- Who took it to analyse. Kept after the proposal, so an accepted idea
  -- still says which agent proposed the solution.
  claimed_by_principal uuid REFERENCES principals (id),
  claimed_by_client    uuid REFERENCES client_credentials (id),
  claimed_at           timestamptz,
  -- A claim is a lease. Past this, the idea is free for the next agent,
  -- so a crashed agent does not hold it forever. Set only while claimed.
  claim_expires_at     timestamptz,
  proposed_at          timestamptz,
  decided_by_principal uuid REFERENCES principals (id),
  decided_at           timestamptz,
  -- The draft plan a proposal came with. A plan deleted later leaves the
  -- idea and its proposal text in place.
  resulting_plan_id    uuid REFERENCES plans (id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version),
  CONSTRAINT ideas_lease_only_while_claimed CHECK ((status = 'claimed') = (claim_expires_at IS NOT NULL))
);

-- The queue: what claim_next and the open list read, in their order.
CREATE INDEX ideas_queue_idx ON ideas (project_id, priority DESC, position, created_at, id)
  WHERE status IN ('open', 'claimed');
CREATE INDEX ideas_project_status_idx ON ideas (project_id, status, updated_at DESC);

ALTER TABLE ideas ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_scoped ON ideas
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- DELETE is withheld: an idea is withdrawn or rejected, never erased, so
-- "what happened to my idea" always has an answer.
GRANT SELECT, INSERT, UPDATE ON ideas TO cc_app;

-- Idea text counts against the namespace quota (docs/adr/0021) by the size
-- of its ciphertext, through the same function plans use: it reads only
-- project_id and ciphertext, which ideas have in the same shape.
CREATE TRIGGER ideas_storage
  AFTER INSERT OR DELETE OR UPDATE OF ciphertext ON ideas
  FOR EACH ROW EXECUTE FUNCTION cc_plan_text_storage();
