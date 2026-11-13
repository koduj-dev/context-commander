-- Memory vectors in plaintext pgvector — docs/adr/0028-semantic-search.md.
--
-- NOT part of the core migration sequence. This set is applied only by
-- `ccd migrate --pgvector`, tracked in its own schema_migrations_pgvector
-- table, on a Postgres that has the pgvector extension (for example the
-- pgvector/pgvector:pg16 image). The core migrations keep working on plain
-- postgres:16-alpine because nothing in them mentions the vector type.
--
-- THE EXCEPTION TO ADR 0011. The embedding column is plaintext. A vector
-- is derived from encrypted text and says what that text is about: two
-- memories about the same thing have close vectors, and a vector can be
-- compared against vectors of guessed texts. Anyone who can read the
-- database (an operator, a backup, a stolen dump) learns that much about
-- every memory. An installation chooses this with
-- --embeddings-store=pgvector to have similarity computed in SQL instead of
-- decrypting vectors into the server's memory. The default stays the
-- encrypted memory_embeddings table (core migration 000040).

-- The extension is not trusted, so cc_owner cannot create it. A superuser
-- runs CREATE EXTENSION vector once (deploy/postgres/init/02-pgvector.sql
-- does it in the compose deployment); `ccd migrate --pgvector` checks for it
-- first and says so when it is missing. This guard makes the failure name
-- the cause rather than "type vector does not exist".
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
    RAISE EXCEPTION 'the pgvector extension is not installed in this database: a superuser must run CREATE EXTENSION vector first';
  END IF;
END
$$;

-- No dimension on the column: the model is configuration, and a model with
-- a different length must be storable without a migration. The price is no
-- approximate (HNSW/IVFFlat) index, which needs a fixed dimension; search
-- compares at most one search's candidates, exactly (docs/adr/0028).
CREATE TABLE memory_vectors (
  memory_id      uuid NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  model          text NOT NULL CHECK (length(model) BETWEEN 1 AND 200),
  project_id     uuid NOT NULL REFERENCES projects (id),
  memory_version int NOT NULL,
  embedding      vector NOT NULL CHECK (vector_dims(embedding) BETWEEN 1 AND 8192),
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (memory_id, model)
);
CREATE INDEX memory_vectors_project_idx ON memory_vectors (project_id, model);

ALTER TABLE memory_vectors ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON memory_vectors
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON memory_vectors TO cc_app;

-- golang-migrate creates its version table before the first migration runs.
-- The server, connected as cc_app, reads it at startup to refuse
-- --embeddings-store=pgvector on a database this set has not reached.
GRANT SELECT ON schema_migrations_pgvector TO cc_app;

-- The same as cc_memory_embeddings_pending (core 000040), over this table.
CREATE FUNCTION cc_memory_vectors_pending(p_model text, p_after uuid, p_limit int, p_all bool)
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
  LEFT JOIN memory_vectors v ON v.memory_id = m.id AND v.model = p_model
  WHERE m.id > p_after
    AND (p_all OR v.memory_id IS NULL OR v.memory_version <> m.version)
  ORDER BY m.id
  LIMIT least(greatest(p_limit, 1), 1000)
$$;

CREATE FUNCTION cc_memory_vectors_pending_count(p_model text)
RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT count(*) FROM memories m
  LEFT JOIN memory_vectors v ON v.memory_id = m.id AND v.model = p_model
  WHERE v.memory_id IS NULL OR v.memory_version <> m.version
$$;

REVOKE ALL ON FUNCTION cc_memory_vectors_pending(text, uuid, int, bool) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_memory_vectors_pending_count(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_memory_vectors_pending(text, uuid, int, bool) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_memory_vectors_pending_count(text) TO cc_app;
