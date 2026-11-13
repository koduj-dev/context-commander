-- pgvector, only where the image has it — docs/adr/0028-semantic-search.md.
--
-- The extension is not marked trusted, so cc_owner (who runs migrations)
-- cannot create it; the initdb superuser does it here, once, when the
-- cluster is created. On the default postgres:16-alpine image the extension
-- is not available and this does nothing, so one init directory serves both
-- images. Only `ccd migrate --pgvector` creates anything that uses it.

\connect context_commander

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector') THEN
    CREATE EXTENSION IF NOT EXISTS vector;
  END IF;
END
$$;
