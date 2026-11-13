-- Memory embeddings, encrypted — docs/adr/0028-semantic-search.md.
--
-- One vector per memory and embedding model. A vector is CONTENT PAYLOAD
-- (docs/adr/0011): it encodes what the text means closely enough that
-- similar texts can be found from it, so it is sealed with the project key
-- like the text it came from. Similarity is computed in the server, over
-- vectors decrypted into a bounded cache, never in SQL.
--
-- The plaintext columns are operational metadata: which memory, which
-- model, which memory version the vector was made from (so an edited memory
-- is embedded again), and its length.
--
-- Installations that choose plaintext pgvector storage instead get their
-- table from the separate migrations/pgvector set; this one is created
-- everywhere because it costs nothing when unused and keeps the core
-- sequence identical on every installation.

CREATE TABLE memory_embeddings (
  memory_id           uuid NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  -- Part of the key, so switching models leaves the old vectors visibly
  -- stale rather than silently comparable with new ones.
  model               text NOT NULL CHECK (length(model) BETWEEN 1 AND 200),
  project_id          uuid NOT NULL REFERENCES projects (id),
  memory_version      int NOT NULL,
  dimensions          int NOT NULL CHECK (dimensions BETWEEN 1 AND 8192),
  ciphertext          bytea NOT NULL,
  nonce               bytea NOT NULL,
  wrapped_dek         bytea NOT NULL,
  project_key_version int NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (memory_id, model),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);
-- Search reads a project's vectors for one model by memory id.
CREATE INDEX memory_embeddings_project_idx ON memory_embeddings (project_id, model);

ALTER TABLE memory_embeddings ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON memory_embeddings
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- UPDATE replaces a stale vector; DELETE drops vectors of a model no longer
-- configured. Deleting a memory removes its vectors by cascade.
GRANT SELECT, INSERT, UPDATE, DELETE ON memory_embeddings TO cc_app;

-- What still needs embedding, for the indexer's sweep and
-- `ccd embeddings reindex`. SECURITY DEFINER because the sweep runs with no
-- request and therefore no principal, and RLS would show it nothing.
--
-- It returns identifiers only — never text or vectors — and, for each
-- memory, a principal the indexer may act as: the author while they are
-- still a member of the project, otherwise its longest-standing owner or
-- member. The indexer then reads and writes as that principal under RLS,
-- through the same authorization as a request, so the function hands out
-- no access the principal does not already have.
CREATE FUNCTION cc_memory_embeddings_pending(p_model text, p_after uuid, p_limit int, p_all bool)
RETURNS TABLE (project_id uuid, memory_id uuid, principal_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT m.project_id, m.id,
         COALESCE(
           (SELECT pm.principal_id FROM project_members pm
             WHERE pm.project_id = m.project_id AND pm.principal_id = m.created_by_principal),
           (SELECT pm.principal_id FROM project_members pm
             WHERE pm.project_id = m.project_id
             ORDER BY (pm.role = 'OWNER') DESC, pm.created_at, pm.principal_id LIMIT 1))
  FROM memories m
  LEFT JOIN memory_embeddings e ON e.memory_id = m.id AND e.model = p_model
  WHERE m.id > p_after
    AND (p_all OR e.memory_id IS NULL OR e.memory_version <> m.version)
  ORDER BY m.id
  LIMIT least(greatest(p_limit, 1), 1000)
$$;

CREATE FUNCTION cc_memory_embeddings_pending_count(p_model text)
RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT count(*) FROM memories m
  LEFT JOIN memory_embeddings e ON e.memory_id = m.id AND e.model = p_model
  WHERE e.memory_id IS NULL OR e.memory_version <> m.version
$$;

REVOKE ALL ON FUNCTION cc_memory_embeddings_pending(text, uuid, int, bool) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_memory_embeddings_pending_count(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_memory_embeddings_pending(text, uuid, int, bool) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_memory_embeddings_pending_count(text) TO cc_app;
