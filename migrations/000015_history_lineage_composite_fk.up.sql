-- history's lineage references (conversation_id, external_session_id,
-- parent_external_session_id, derived_from) were each an independent
-- single-column foreign key with no requirement that the referenced row
-- share the new row's project_id (or, for external_session_id/derived_from,
-- its conversation_id either). Service methods authorized only projectID
-- and inserted every supplied ID as-is, so a contributor to project A could
-- create a session/history object in A that referenced a conversation,
-- parent session, external session, or derived-from object belonging to
-- project B — corrupting lineage boundaries, and making insert
-- success/failure an existence oracle for otherwise-hidden UUIDs in other
-- projects.
--
-- Composite unique keys let every lineage FK below require project_id (and
-- conversation_id, where the reference is conversation-scoped) to match —
-- same technique as history_objects' existing
-- (project_id, project_key_version) -> project_keys FK.

ALTER TABLE conversations ADD CONSTRAINT conversations_project_id_id_key UNIQUE (project_id, id);

ALTER TABLE external_sessions ADD CONSTRAINT external_sessions_project_conversation_id_key UNIQUE (project_id, conversation_id, id);

ALTER TABLE history_objects ADD CONSTRAINT history_objects_project_conversation_id_key UNIQUE (project_id, conversation_id, id);

ALTER TABLE external_sessions DROP CONSTRAINT external_sessions_conversation_id_fkey;
ALTER TABLE external_sessions
  ADD CONSTRAINT external_sessions_project_conversation_fkey
  FOREIGN KEY (project_id, conversation_id) REFERENCES conversations (project_id, id);

-- parent_external_session_id is nullable — MATCH SIMPLE (Postgres's
-- default) skips the check entirely when any FK column is NULL, so this
-- still allows "no parent" while requiring a *given* parent to share both
-- project_id and conversation_id with the child row.
ALTER TABLE external_sessions DROP CONSTRAINT external_sessions_parent_external_session_id_fkey;
ALTER TABLE external_sessions
  ADD CONSTRAINT external_sessions_parent_project_conversation_fkey
  FOREIGN KEY (project_id, conversation_id, parent_external_session_id) REFERENCES external_sessions (project_id, conversation_id, id);

ALTER TABLE history_objects DROP CONSTRAINT history_objects_conversation_id_fkey;
ALTER TABLE history_objects
  ADD CONSTRAINT history_objects_project_conversation_fkey
  FOREIGN KEY (project_id, conversation_id) REFERENCES conversations (project_id, id);

ALTER TABLE history_objects DROP CONSTRAINT history_objects_external_session_id_fkey;
ALTER TABLE history_objects
  ADD CONSTRAINT history_objects_external_session_project_conversation_fkey
  FOREIGN KEY (project_id, conversation_id, external_session_id) REFERENCES external_sessions (project_id, conversation_id, id);

-- derived_from is nullable — same MATCH SIMPLE reasoning as
-- parent_external_session_id above.
ALTER TABLE history_objects DROP CONSTRAINT history_objects_derived_from_fkey;
ALTER TABLE history_objects
  ADD CONSTRAINT history_objects_derived_from_project_conversation_fkey
  FOREIGN KEY (project_id, conversation_id, derived_from) REFERENCES history_objects (project_id, conversation_id, id);
