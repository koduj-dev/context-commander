-- Plans, phases and presence — docs/adr/0022-plans-phases-and-presence.md.
--
-- Text is CONTENT PAYLOAD (docs/adr/0011) and lives encrypted on the phase
-- and plan rows. A plan's blob carries its own text AND every item's text,
-- so an item row is metadata only: ticking it re-encrypts nothing. Status,
-- kind, position, assignee and timestamps are OPERATIONAL METADATA and stay
-- plaintext, which is what lets the board be counted without decrypting.
--
-- Growth is bounded on purpose, because agents under pressure write a lot:
-- text has no history, status changes keep the last 20 per item, effort is
-- one aggregated row per (item, principal), presence one row per client.

CREATE TABLE phases (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  position             int NOT NULL,
  status               text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'active', 'done')),
  version              int NOT NULL DEFAULT 1,
  ciphertext           bytea NOT NULL,
  nonce                bytea NOT NULL,
  wrapped_dek          bytea NOT NULL,
  project_key_version  int NOT NULL,
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);
CREATE INDEX phases_project_idx ON phases (project_id, position);

CREATE TABLE plans (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  -- Optional: an agent starting a task need not know where it belongs.
  phase_id             uuid REFERENCES phases (id) ON DELETE SET NULL,
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'done', 'abandoned')),
  -- Bumped on every rewrite of the blob; also the AAD's ResourceVersion.
  version              int NOT NULL DEFAULT 1,
  ciphertext           bytea NOT NULL,
  nonce                bytea NOT NULL,
  wrapped_dek          bytea NOT NULL,
  project_key_version  int NOT NULL,
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_by_client    uuid REFERENCES client_credentials (id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);
CREATE INDEX plans_project_idx ON plans (project_id, updated_at DESC);
CREATE INDEX plans_phase_idx ON plans (phase_id);

CREATE TABLE plan_items (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  plan_id              uuid NOT NULL REFERENCES plans (id) ON DELETE CASCADE,
  position             int NOT NULL,
  kind                 text NOT NULL DEFAULT 'task' CHECK (kind IN ('task', 'check')),
  status               text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'done', 'blocked', 'skipped')),
  assignee_principal   uuid REFERENCES principals (id),
  updated_by_principal uuid REFERENCES principals (id),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  completed_at         timestamptz
);
CREATE INDEX plan_items_plan_idx ON plan_items (plan_id, position);
CREATE INDEX plan_items_project_status_idx ON plan_items (project_id, status);

-- The last 20 status changes per item (internal/plans prunes on insert).
CREATE TABLE plan_item_transitions (
  id           bigserial PRIMARY KEY,
  project_id   uuid NOT NULL REFERENCES projects (id),
  item_id      uuid NOT NULL REFERENCES plan_items (id) ON DELETE CASCADE,
  from_status  text NOT NULL,
  to_status    text NOT NULL,
  principal_id uuid NOT NULL REFERENCES principals (id),
  client_id    uuid REFERENCES client_credentials (id),
  at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plan_item_transitions_item_idx ON plan_item_transitions (item_id, id DESC);

-- Aggregated, never logged: time covered by heartbeats and the tokens the
-- client reported, per item and principal.
CREATE TABLE plan_item_effort (
  item_id      uuid NOT NULL REFERENCES plan_items (id) ON DELETE CASCADE,
  principal_id uuid NOT NULL REFERENCES principals (id),
  project_id   uuid NOT NULL REFERENCES projects (id),
  seconds      bigint NOT NULL DEFAULT 0 CHECK (seconds >= 0),
  tokens_in    bigint NOT NULL DEFAULT 0 CHECK (tokens_in >= 0),
  tokens_out   bigint NOT NULL DEFAULT 0 CHECK (tokens_out >= 0),
  PRIMARY KEY (item_id, principal_id)
);
CREATE INDEX plan_item_effort_project_idx ON plan_item_effort (project_id);

-- Where each client is. One row per credential, overwritten by every report.
CREATE TABLE work_presence (
  client_id           uuid PRIMARY KEY REFERENCES client_credentials (id) ON DELETE CASCADE,
  principal_id        uuid NOT NULL REFERENCES principals (id),
  project_id          uuid NOT NULL REFERENCES projects (id),
  plan_id             uuid REFERENCES plans (id) ON DELETE SET NULL,
  item_id             uuid REFERENCES plan_items (id) ON DELETE SET NULL,
  -- The "what I am doing" line, encrypted; NULL when none was given.
  ciphertext          bytea,
  nonce               bytea,
  wrapped_dek         bytea,
  project_key_version int,
  -- When the client arrived at item_id, and when it last reported.
  since               timestamptz NOT NULL DEFAULT now(),
  last_beat           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);
CREATE INDEX work_presence_project_idx ON work_presence (project_id, last_beat DESC);

ALTER TABLE phases ENABLE ROW LEVEL SECURITY;
ALTER TABLE plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE plan_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE plan_item_transitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE plan_item_effort ENABLE ROW LEVEL SECURITY;
ALTER TABLE work_presence ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_scoped ON phases
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
CREATE POLICY project_scoped ON plans
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
CREATE POLICY project_scoped ON plan_items
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
CREATE POLICY project_scoped ON plan_item_transitions
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
CREATE POLICY project_scoped ON plan_item_effort
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
CREATE POLICY project_scoped ON work_presence
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON phases TO cc_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON plans TO cc_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON plan_items TO cc_app;
-- No UPDATE: a recorded change is a fact. DELETE only for the pruning.
GRANT SELECT, INSERT, DELETE ON plan_item_transitions TO cc_app;
GRANT USAGE ON SEQUENCE plan_item_transitions_id_seq TO cc_app;
GRANT SELECT, INSERT, UPDATE ON plan_item_effort TO cc_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON work_presence TO cc_app;

-- Presence idle for a week goes, on the next report anyone makes. SECURITY
-- DEFINER because the reporter's RLS view covers only their own projects,
-- and a project nobody reports in any more is exactly where stale rows sit.
CREATE FUNCTION cc_prune_presence()
RETURNS void
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
  DELETE FROM work_presence WHERE last_beat < now() - interval '7 days';
$$;
REVOKE ALL ON FUNCTION cc_prune_presence() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_prune_presence() TO cc_app;

-- Plan and phase text counts against the namespace quota (docs/adr/0021),
-- by the size of the ciphertext, as history does. An UPDATE charges the
-- difference, so a plan that grew is refused where it does not fit and one
-- that shrank gives the bytes back.
CREATE FUNCTION cc_plan_text_storage()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM cc_charge_storage(cc_project_namespace(NEW.project_id), octet_length(NEW.ciphertext));
    RETURN NEW;
  ELSIF TG_OP = 'UPDATE' THEN
    PERFORM cc_charge_storage(cc_project_namespace(NEW.project_id),
      octet_length(NEW.ciphertext) - octet_length(OLD.ciphertext));
    RETURN NEW;
  END IF;
  PERFORM cc_charge_storage(cc_project_namespace(OLD.project_id), -octet_length(OLD.ciphertext));
  RETURN OLD;
END;
$$;

CREATE TRIGGER plans_storage
  AFTER INSERT OR DELETE OR UPDATE OF ciphertext ON plans
  FOR EACH ROW EXECUTE FUNCTION cc_plan_text_storage();
CREATE TRIGGER phases_storage
  AFTER INSERT OR DELETE OR UPDATE OF ciphertext ON phases
  FOR EACH ROW EXECUTE FUNCTION cc_plan_text_storage();
