DROP FUNCTION IF EXISTS cc_memory_vectors_pending_count(text);
DROP FUNCTION IF EXISTS cc_memory_vectors_pending(text, uuid, int, bool);
DROP TABLE IF EXISTS memory_vectors;
-- The extension stays: a superuser created it, and other databases or tools
-- on the cluster may use it.
