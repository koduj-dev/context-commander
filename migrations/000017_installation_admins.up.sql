-- Installation-admin authorization tier — docs/adr/0017-installation-admin-tier.md.
--
-- Mirrors organization_members' shape (migrations/000001): keyed by
-- user_id, not principal_id, because a ServicePrincipal can never hold
-- installation-admin rights (same rationale as docs/adr/0004 gives for
-- organization membership, and more load-bearing here — an installation
-- admin can create arbitrary users and register arbitrary service
-- principals, so the blast radius of "an agent could hold this" is the
-- whole instance, not one organization).
--
-- No RLS: same trust model as users/principals/client_credentials
-- (migrations/000001, all RLS-free) — this table is never queried directly
-- by adapter code, only through internal/installation.Store, which every
-- caller reaches via internal/installation.Service's capability check
-- (docs/adr/0014) or, for granting the very first admin (a chicken-and-egg
-- problem no authenticated principal can solve), through `ccd installation
-- grant-admin`, an operator/--database-url-only CLI path with the same
-- trust level as `ccd migrate` and `ccd bootstrap`.
--
-- role is a single value today (only ADMIN exists), CHECK-constrained
-- rather than left off entirely so a future narrower tier (e.g. a
-- read-only AUDITOR) is an INSERT/UPDATE-time decision, not a migration.
CREATE TABLE installation_admins (
  user_id    uuid PRIMARY KEY REFERENCES users (id),
  role       text NOT NULL DEFAULT 'ADMIN' CHECK (role IN ('ADMIN')),
  granted_by uuid REFERENCES users (id),
  granted_at timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, DELETE ON installation_admins TO cc_app;
