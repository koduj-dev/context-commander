-- Storage quotas per namespace — docs/adr/0021-storage-quotas-per-namespace.md.
--
-- The database keeps the count, by trigger, so that a write path added later
-- is counted whether or not its author knew this table exists. The same
-- trigger refuses a write that takes the namespace over its limit: the
-- charge and the insert are one transaction, and the row lock on
-- namespace_storage is what stops two concurrent writes from both fitting
-- under the limit.

CREATE TABLE namespace_storage (
  namespace_id uuid PRIMARY KEY REFERENCES namespaces (id) ON DELETE CASCADE,
  used_bytes   bigint NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
  -- An installation admin's exception to the server default. NULL with
  -- unlimited = false means "the default applies".
  limit_bytes  bigint CHECK (limit_bytes >= 0),
  unlimited    boolean NOT NULL DEFAULT false,
  limit_set_by uuid REFERENCES users (id),
  limit_set_at timestamptz,
  CHECK (NOT (unlimited AND limit_bytes IS NOT NULL))
);
-- No grant to cc_app: written by the triggers and cc_set_namespace_quota(),
-- read through the functions below.

-- Measured from migration 000030 on. Versions published earlier keep 0: their
-- size is in the object store, which SQL cannot see, and 0 is a known
-- undercount where any other number would be a guess.
ALTER TABLE skill_versions ADD COLUMN size_bytes bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0);

-- The server default, which db.WithPrincipal sets per transaction from
-- --storage-quota. Unset (or set to '') means no default limit. A custom
-- setting that was once set on a pooled connection reads back as '' after
-- its transaction ends, hence the NULLIF.
CREATE FUNCTION cc_storage_default()
RETURNS bigint
LANGUAGE sql STABLE
AS $$
  SELECT NULLIF(current_setting('app.storage_quota_default', true), '')::bigint;
$$;

-- The limit that applies to a namespace: none if an admin made it
-- unlimited, the admin's number if there is one, else the server default.
CREATE FUNCTION cc_storage_effective_limit(unlimited boolean, limit_bytes bigint)
RETURNS bigint
LANGUAGE sql STABLE
AS $$
  SELECT CASE WHEN unlimited THEN NULL ELSE COALESCE(limit_bytes, cc_storage_default()) END;
$$;

CREATE FUNCTION cc_storage_quota_exceeded()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
  -- CCQ01 is ours: internal/quota maps it to quota.ErrExceeded. No numbers in
  -- the message, because it reaches whoever was writing, and a project
  -- member outside the owning organization has no business learning its
  -- total.
  RAISE EXCEPTION 'storage quota exceeded' USING ERRCODE = 'CCQ01';
END;
$$;

-- Adds delta to a namespace's count and, when it grew, refuses the write if
-- that took it over the limit. A negative delta is never refused: a
-- namespace over its limit (an admin lowered it) must still be able to
-- delete its way back.
CREATE FUNCTION cc_charge_storage(ns uuid, delta bigint)
RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  used bigint;
  lim  bigint;
BEGIN
  IF ns IS NULL OR delta = 0 THEN
    RETURN;
  END IF;
  INSERT INTO namespace_storage AS s (namespace_id, used_bytes)
  VALUES (ns, GREATEST(delta, 0))
  ON CONFLICT (namespace_id) DO UPDATE
    SET used_bytes = GREATEST(s.used_bytes + delta, 0)
  RETURNING s.used_bytes, cc_storage_effective_limit(s.unlimited, s.limit_bytes)
  INTO used, lim;
  IF delta > 0 AND lim IS NOT NULL AND used > lim THEN
    PERFORM cc_storage_quota_exceeded();
  END IF;
END;
$$;

-- Would adding bytes to this namespace go over its limit? Advisory: it lets
-- an artifact write or a skill publish refuse before uploading to the object
-- store, and the trigger still decides at insert.
CREATE FUNCTION cc_check_storage(ns uuid, bytes bigint)
RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  used bigint := 0;
  lim  bigint := cc_storage_default();
BEGIN
  IF ns IS NULL THEN
    RETURN;
  END IF;
  SELECT s.used_bytes, cc_storage_effective_limit(s.unlimited, s.limit_bytes)
  INTO used, lim
  FROM namespace_storage s WHERE s.namespace_id = ns;
  IF NOT FOUND THEN
    used := 0;
    lim := cc_storage_default();
  END IF;
  IF lim IS NOT NULL AND used + bytes > lim THEN
    PERFORM cc_storage_quota_exceeded();
  END IF;
END;
$$;

CREATE FUNCTION cc_project_namespace(project uuid)
RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT namespace_id FROM projects WHERE id = project;
$$;

-- The namespace a skill scope belongs to. An installation-scoped skill
-- belongs to none and is not counted (docs/adr/0021).
CREATE FUNCTION cc_skill_scope_namespace(scope text, scope_ref uuid)
RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT CASE scope
    WHEN 'project'      THEN (SELECT namespace_id FROM projects WHERE id = scope_ref)
    WHEN 'organization' THEN (SELECT id FROM namespaces WHERE organization_id = scope_ref)
  END;
$$;

CREATE FUNCTION cc_check_project_storage(project uuid, bytes bigint)
RETURNS void
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT cc_check_storage(cc_project_namespace(project), bytes);
$$;

CREATE FUNCTION cc_check_skill_storage(scope text, scope_ref uuid, bytes bigint)
RETURNS void
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT cc_check_storage(cc_skill_scope_namespace(scope, scope_ref), bytes);
$$;

-- ---------------------------------------------------------------------------
-- Triggers
-- ---------------------------------------------------------------------------

-- Artifact versions count while their artifact is live. A soft delete gives
-- the bytes back (see cc_artifact_deleted below), so a version removed later
-- by a purge must not give them back a second time.
CREATE FUNCTION cc_artifact_version_storage()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM cc_charge_storage(cc_project_namespace(NEW.project_id), NEW.size_bytes);
    RETURN NEW;
  END IF;
  IF EXISTS (SELECT 1 FROM artifacts WHERE id = OLD.artifact_id AND deleted_at IS NULL) THEN
    PERFORM cc_charge_storage(cc_project_namespace(OLD.project_id), -OLD.size_bytes);
  END IF;
  RETURN OLD;
END;
$$;

CREATE TRIGGER artifact_versions_storage
  AFTER INSERT OR DELETE ON artifact_versions
  FOR EACH ROW EXECUTE FUNCTION cc_artifact_version_storage();

CREATE FUNCTION cc_artifact_deleted()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
    PERFORM cc_charge_storage(cc_project_namespace(NEW.project_id),
      -(SELECT COALESCE(sum(size_bytes), 0) FROM artifact_versions WHERE artifact_id = NEW.id)::bigint);
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER artifacts_deleted_storage
  AFTER UPDATE OF deleted_at ON artifacts
  FOR EACH ROW EXECUTE FUNCTION cc_artifact_deleted();

CREATE FUNCTION cc_history_object_storage()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM cc_charge_storage(cc_project_namespace(NEW.project_id), octet_length(NEW.ciphertext));
    RETURN NEW;
  END IF;
  PERFORM cc_charge_storage(cc_project_namespace(OLD.project_id), -octet_length(OLD.ciphertext));
  RETURN OLD;
END;
$$;

CREATE TRIGGER history_objects_storage
  AFTER INSERT OR DELETE ON history_objects
  FOR EACH ROW EXECUTE FUNCTION cc_history_object_storage();

CREATE FUNCTION cc_skill_version_storage()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  sk skills%ROWTYPE;
BEGIN
  IF TG_OP = 'INSERT' THEN
    SELECT * INTO sk FROM skills WHERE id = NEW.skill_id;
    PERFORM cc_charge_storage(cc_skill_scope_namespace(sk.scope, sk.scope_ref), NEW.size_bytes);
    RETURN NEW;
  END IF;
  SELECT * INTO sk FROM skills WHERE id = OLD.skill_id;
  IF FOUND THEN
    PERFORM cc_charge_storage(cc_skill_scope_namespace(sk.scope, sk.scope_ref), -OLD.size_bytes);
  END IF;
  RETURN OLD;
END;
$$;

CREATE TRIGGER skill_versions_storage
  AFTER INSERT OR DELETE ON skill_versions
  FOR EACH ROW EXECUTE FUNCTION cc_skill_version_storage();

-- A promoted skill takes its bytes with it. The charge comes before the
-- credit so a promotion into a namespace without room is refused.
CREATE FUNCTION cc_skill_scope_storage()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  from_ns uuid := cc_skill_scope_namespace(OLD.scope, OLD.scope_ref);
  to_ns   uuid := cc_skill_scope_namespace(NEW.scope, NEW.scope_ref);
  total   bigint;
BEGIN
  IF from_ns IS NOT DISTINCT FROM to_ns THEN
    RETURN NEW;
  END IF;
  SELECT COALESCE(sum(size_bytes), 0) INTO total FROM skill_versions WHERE skill_id = NEW.id;
  PERFORM cc_charge_storage(to_ns, total);
  PERFORM cc_charge_storage(from_ns, -total);
  RETURN NEW;
END;
$$;

CREATE TRIGGER skills_scope_storage
  AFTER UPDATE OF scope, scope_ref ON skills
  FOR EACH ROW EXECUTE FUNCTION cc_skill_scope_storage();

-- Nothing moves a project between namespaces today. If something ever does,
-- its bytes go with it rather than staying charged to the old owner.
CREATE FUNCTION cc_project_namespace_storage()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  total bigint;
BEGIN
  IF OLD.namespace_id = NEW.namespace_id THEN
    RETURN NEW;
  END IF;
  SELECT
    (SELECT COALESCE(sum(v.size_bytes), 0) FROM artifact_versions v JOIN artifacts a ON a.id = v.artifact_id
      WHERE v.project_id = NEW.id AND a.deleted_at IS NULL)
  + (SELECT COALESCE(sum(octet_length(ciphertext)), 0) FROM history_objects WHERE project_id = NEW.id)
  + (SELECT COALESCE(sum(v.size_bytes), 0) FROM skill_versions v JOIN skills s ON s.id = v.skill_id
      WHERE s.scope = 'project' AND s.scope_ref = NEW.id)
  INTO total;
  PERFORM cc_charge_storage(NEW.namespace_id, total);
  PERFORM cc_charge_storage(OLD.namespace_id, -total);
  RETURN NEW;
END;
$$;

CREATE TRIGGER projects_namespace_storage
  AFTER UPDATE OF namespace_id ON projects
  FOR EACH ROW EXECUTE FUNCTION cc_project_namespace_storage();

-- ---------------------------------------------------------------------------
-- Reads and the admin's exception
-- ---------------------------------------------------------------------------

-- Every namespace with its usage, for an installation admin. Authorization
-- is internal/installation's (docs/adr/0014), the same as
-- cc_installation_stats(). Slug, a display name and numbers: never a project
-- or a path inside the namespace (migrations/000024's boundary).
CREATE FUNCTION cc_namespace_storage_page(after_created timestamptz, after_id uuid, page_size int)
RETURNS TABLE (
  namespace_id    uuid,
  slug            text,
  kind            text,
  display_name    text,
  created_at      timestamptz,
  used_bytes      bigint,
  limit_bytes     bigint,
  unlimited       boolean,
  effective_limit bigint
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT n.id, n.slug, n.kind,
         COALESCE(o.name, u.display_name, n.slug),
         n.created_at,
         COALESCE(s.used_bytes, 0),
         s.limit_bytes,
         COALESCE(s.unlimited, false),
         cc_storage_effective_limit(COALESCE(s.unlimited, false), s.limit_bytes)
  FROM namespaces n
  LEFT JOIN namespace_storage s ON s.namespace_id = n.id
  LEFT JOIN organizations o ON o.id = n.organization_id
  LEFT JOIN users u ON u.id = n.owner_user_id
  WHERE after_id IS NULL OR (n.created_at, n.id) > (after_created, after_id)
  ORDER BY n.created_at, n.id
  LIMIT page_size;
$$;

-- The caller's own namespace and those of their organizations. Keyed on the
-- transaction's principal, never on an argument, so it cannot be pointed at
-- someone else's.
CREATE FUNCTION cc_my_storage()
RETURNS TABLE (
  namespace_id    uuid,
  slug            text,
  kind            text,
  display_name    text,
  used_bytes      bigint,
  effective_limit bigint
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  WITH me AS (
    SELECT user_id FROM principals
    WHERE id = NULLIF(current_setting('app.current_principal_id', true), '')::uuid
      AND user_id IS NOT NULL
  )
  SELECT n.id, n.slug, n.kind,
         COALESCE(o.name, u.display_name, n.slug),
         COALESCE(s.used_bytes, 0),
         cc_storage_effective_limit(COALESCE(s.unlimited, false), s.limit_bytes)
  FROM namespaces n
  LEFT JOIN namespace_storage s ON s.namespace_id = n.id
  LEFT JOIN organizations o ON o.id = n.organization_id
  LEFT JOIN users u ON u.id = n.owner_user_id
  WHERE n.owner_user_id IN (SELECT user_id FROM me)
     OR n.organization_id IN (
       SELECT m.organization_id FROM organization_members m WHERE m.user_id IN (SELECT user_id FROM me))
  ORDER BY n.kind DESC, n.slug;
$$;

-- An installation admin's exception. limit NULL with unlimited false puts
-- the namespace back on the server default. Returns false for a namespace
-- that does not exist.
CREATE FUNCTION cc_set_namespace_quota(ns uuid, new_limit bigint, no_limit boolean, set_by uuid)
RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM namespaces WHERE id = ns) THEN
    RETURN false;
  END IF;
  INSERT INTO namespace_storage AS s (namespace_id, limit_bytes, unlimited, limit_set_by, limit_set_at)
  VALUES (ns, new_limit, no_limit, set_by, now())
  ON CONFLICT (namespace_id) DO UPDATE
    SET limit_bytes = EXCLUDED.limit_bytes, unlimited = EXCLUDED.unlimited,
        limit_set_by = EXCLUDED.limit_set_by, limit_set_at = EXCLUDED.limit_set_at;
  RETURN true;
END;
$$;

REVOKE ALL ON FUNCTION cc_charge_storage(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_check_storage(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_project_namespace(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_skill_scope_namespace(text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_check_project_storage(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_check_skill_storage(text, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_namespace_storage_page(timestamptz, uuid, int) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_my_storage() FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_set_namespace_quota(uuid, bigint, boolean, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_check_project_storage(uuid, bigint) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_check_skill_storage(text, uuid, bigint) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_namespace_storage_page(timestamptz, uuid, int) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_my_storage() TO cc_app;
GRANT EXECUTE ON FUNCTION cc_set_namespace_quota(uuid, bigint, boolean, uuid) TO cc_app;

-- What is already stored, counted the way the triggers will count from now
-- on. No limit applies here: this records the past, it does not refuse it.
INSERT INTO namespace_storage (namespace_id, used_bytes)
SELECT n.id,
    (SELECT COALESCE(sum(v.size_bytes), 0) FROM artifact_versions v
       JOIN artifacts a ON a.id = v.artifact_id JOIN projects p ON p.id = v.project_id
      WHERE p.namespace_id = n.id AND a.deleted_at IS NULL)
  + (SELECT COALESCE(sum(octet_length(h.ciphertext)), 0) FROM history_objects h
       JOIN projects p ON p.id = h.project_id WHERE p.namespace_id = n.id)
FROM namespaces n;
