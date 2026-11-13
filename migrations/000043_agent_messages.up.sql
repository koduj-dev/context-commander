-- Messages to agents mid-work — docs/adr/0032-agent-inbox.md.
--
-- A person sends an agent something while it works ("a thought just came to
-- me"). MCP is pull-based, so a message waits here until the recipient's
-- next tool call or client hook picks it up.
--
-- The body is CONTENT PAYLOAD (docs/adr/0011), encrypted on the row with the
-- project key. Who sent it, who it is for, and when are OPERATIONAL METADATA.
--
-- Addressing, by which recipient columns are set:
--   recipient_client set     one session (a client credential, as presence
--                            knows it); recipient_principal is then that
--                            credential's principal, always.
--   recipient_principal only every session of that principal in the project.
--   neither                  everyone working on the project.
-- Keeping recipient_principal set for a session message is what lets the
-- RLS policy below say "addressed to you" without a subquery.

CREATE TABLE agent_messages (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- The inbox cursor. A sequence rather than created_at: two messages can
  -- share a timestamp, and a cursor needs one direction and no ties.
  seq                  bigserial NOT NULL UNIQUE,
  project_id           uuid NOT NULL REFERENCES projects (id),
  sender_principal     uuid NOT NULL REFERENCES principals (id),
  sender_client        uuid REFERENCES client_credentials (id) ON DELETE SET NULL,
  recipient_principal  uuid REFERENCES principals (id),
  recipient_client     uuid REFERENCES client_credentials (id) ON DELETE CASCADE,
  ciphertext           bytea NOT NULL,
  nonce                bytea NOT NULL,
  wrapped_dek          bytea NOT NULL,
  project_key_version  int NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  expires_at           timestamptz NOT NULL,
  CHECK (recipient_client IS NULL OR recipient_principal IS NOT NULL),
  CHECK (expires_at > created_at),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);
-- The inbox: messages for a principal, and broadcasts per project, by seq.
CREATE INDEX agent_messages_recipient_idx ON agent_messages (recipient_principal, seq) WHERE recipient_principal IS NOT NULL;
CREATE INDEX agent_messages_broadcast_idx ON agent_messages (project_id, seq) WHERE recipient_principal IS NULL;
-- The sender's list in a project, newest first.
CREATE INDEX agent_messages_sender_idx ON agent_messages (project_id, sender_principal, created_at DESC, id DESC);
CREATE INDEX agent_messages_expires_idx ON agent_messages (expires_at);

-- One row per (message, session) once the message was handed to that
-- session. A broadcast reaches many sessions and each sees it once: the row
-- is what says this session already has it. sender_principal and
-- recipient_principal are copied from the message so the policy needs no
-- subquery into agent_messages.
CREATE TABLE agent_message_deliveries (
  message_id          uuid NOT NULL REFERENCES agent_messages (id) ON DELETE CASCADE,
  client_id           uuid NOT NULL REFERENCES client_credentials (id) ON DELETE CASCADE,
  project_id          uuid NOT NULL REFERENCES projects (id),
  recipient_principal uuid NOT NULL REFERENCES principals (id),
  sender_principal    uuid NOT NULL REFERENCES principals (id),
  delivered_at        timestamptz NOT NULL DEFAULT now(),
  acked_at            timestamptz,
  PRIMARY KEY (message_id, client_id)
);
CREATE INDEX agent_message_deliveries_client_idx ON agent_message_deliveries (client_id);

ALTER TABLE agent_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_message_deliveries ENABLE ROW LEVEL SECURITY;

-- A project member sees a message they sent, one addressed to them, and a
-- broadcast. Another member's direct message is not theirs to read, even
-- though they share the project.
CREATE POLICY agent_messages_select ON agent_messages FOR SELECT
  USING (
    cc_has_project_access(current_setting('app.current_principal_id', true), project_id)
    AND (
      sender_principal::text = current_setting('app.current_principal_id', true)
      OR recipient_principal IS NULL
      OR recipient_principal::text = current_setting('app.current_principal_id', true)
    )
  );
-- Nobody sends in someone else's name.
CREATE POLICY agent_messages_insert ON agent_messages FOR INSERT
  WITH CHECK (
    cc_has_project_access(current_setting('app.current_principal_id', true), project_id)
    AND sender_principal::text = current_setting('app.current_principal_id', true)
  );

-- The recipient records its own delivery and ack; the sender reads them to
-- see whether the message arrived.
CREATE POLICY agent_message_deliveries_select ON agent_message_deliveries FOR SELECT
  USING (
    cc_has_project_access(current_setting('app.current_principal_id', true), project_id)
    AND (
      recipient_principal::text = current_setting('app.current_principal_id', true)
      OR sender_principal::text = current_setting('app.current_principal_id', true)
    )
  );
CREATE POLICY agent_message_deliveries_insert ON agent_message_deliveries FOR INSERT
  WITH CHECK (
    cc_has_project_access(current_setting('app.current_principal_id', true), project_id)
    AND recipient_principal::text = current_setting('app.current_principal_id', true)
  );
CREATE POLICY agent_message_deliveries_update ON agent_message_deliveries FOR UPDATE
  USING (
    cc_has_project_access(current_setting('app.current_principal_id', true), project_id)
    AND recipient_principal::text = current_setting('app.current_principal_id', true)
  );

-- No UPDATE or DELETE on messages: a sent message is not edited, and old
-- ones go through cc_prune_agent_messages below. Deliveries are inserted and
-- then acked, never removed by hand.
GRANT SELECT, INSERT ON agent_messages TO cc_app;
GRANT USAGE ON SEQUENCE agent_messages_seq_seq TO cc_app;
GRANT SELECT, INSERT, UPDATE ON agent_message_deliveries TO cc_app;

-- Messages go 30 days after they expired, on the next send anyone makes —
-- the ride-on-the-write pattern presence uses. The 30 days keep the sender's
-- "delivered / acknowledged" list readable after a message stopped being
-- offered. SECURITY DEFINER because a sender's RLS view is their own
-- projects, and stale rows sit exactly where nobody sends any more.
CREATE FUNCTION cc_prune_agent_messages()
RETURNS void
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
  DELETE FROM agent_messages WHERE expires_at < now() - interval '30 days';
$$;
REVOKE ALL ON FUNCTION cc_prune_agent_messages() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_prune_agent_messages() TO cc_app;

-- The body counts against the namespace quota (docs/adr/0021) by the size
-- of its ciphertext, through the same function plans use.
CREATE TRIGGER agent_messages_storage
  AFTER INSERT OR DELETE ON agent_messages
  FOR EACH ROW EXECUTE FUNCTION cc_plan_text_storage();
