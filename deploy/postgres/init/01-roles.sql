-- DB role model — see docs/adr/0002-non-recursive-rls.md.
--
-- cc_owner    : owns tables/functions. Migrations run as this role. As the
--               owner of a table it is exempt from that table's own RLS
--               policies by default (unless FORCE ROW LEVEL SECURITY is set),
--               which is exactly what lets cc_has_project_access() avoid the
--               self-recursive-policy problem.
-- cc_app      : runtime role used by the Go server. NEVER owner, NEVER
--               BYPASSRLS. Gets narrow GRANTs only, applied per-migration.
--
-- Local dev passwords are intentionally simple placeholders — this file only
-- runs inside the disposable docker-compose Postgres container.

CREATE ROLE cc_owner NOSUPERUSER NOBYPASSRLS LOGIN PASSWORD 'cc_owner_dev_password';
CREATE ROLE cc_app   NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE LOGIN PASSWORD 'cc_app_dev_password';

ALTER DATABASE context_commander OWNER TO cc_owner;

\connect context_commander

-- Application schema objects are created by golang-migrate running AS
-- cc_owner (see Makefile `migrate` target), not as the initdb superuser.
GRANT ALL ON SCHEMA public TO cc_owner;
GRANT USAGE ON SCHEMA public TO cc_app;

-- Table/sequence GRANTs for cc_app are issued per-migration once each table
-- exists (see migrations/0001_*), so a forgotten GRANT fails loudly instead
-- of cc_app silently having no access.
