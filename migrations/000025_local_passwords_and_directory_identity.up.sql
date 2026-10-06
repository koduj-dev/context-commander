-- Browser authentication: local passwords and directory identity —
-- docs/adr/0019-browser-authentication-and-directory-identity.md.
--
-- No RLS on either table: same trust model as
-- users/principals/client_credentials (migrations/000001) and
-- installation_admins (migrations/000017). Both are read on the
-- authentication path, which runs through db.WithoutPrincipal by necessity —
-- there is no principal yet, that is the thing being established — so an RLS
-- policy keyed on the current principal could only ever deny. Protection is
-- the GRANTs below plus the fact that nothing outside internal/identity
-- queries these tables.
--
-- A browser session needs no table of its own: it is a client_credentials
-- row with client_type = 'session' and a non-null expires_at, so it is
-- generated, verified, revoked and rate-limited by the code that already
-- does all four for PATs (docs/adr/0009, and 0019's first decision).

-- ---------------------------------------------------------------------------
-- Local passwords
-- ---------------------------------------------------------------------------

-- A separate table rather than a column on users, so a person who
-- authenticates only through a directory has no row here at all. "Has a local
-- password" is then a fact you can see rather than one inferred from a NULL,
-- and withdrawing local login from a directory-managed account is a DELETE
-- rather than a flag that something else has to remember to check.
--
-- password_hash is a PHC string ($argon2id$v=19$m=...,t=...,p=...$salt$hash),
-- so the cost parameters travel with the hash: raising them later is a
-- re-hash on next successful login, not a migration. algorithm is stored
-- alongside anyway — redundant with the PHC prefix on purpose, because it is
-- what a query can filter on when the day comes to find every hash still on
-- the old scheme.
CREATE TABLE user_passwords (
  user_id       uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  password_hash text NOT NULL,
  algorithm     text NOT NULL DEFAULT 'argon2id' CHECK (algorithm IN ('argon2id')),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE, DELETE ON user_passwords TO cc_app;

-- ---------------------------------------------------------------------------
-- Directory identity
-- ---------------------------------------------------------------------------

-- One mapping for both LDAP and OIDC: authenticate against the directory,
-- look up (provider, subject), create the user and this row on first success,
-- then issue a session exactly as a password login does.
--
-- subject is the directory's own stable identifier — entryUUID for LDAP, the
-- `sub` claim for OIDC — and never the email address. An email is a label a
-- person can change and an administrator can reassign to someone else; keying
-- on it is how one person silently inherits another's projects.
--
-- provider is free text naming the configured provider ('ldap', or an OIDC
-- provider's configured id), not a CHECK-constrained enum: the set is decided
-- by an operator's configuration file, so a new provider must not require a
-- migration. The PRIMARY KEY is (provider, subject) because subjects are only
-- unique within the directory that issued them.
CREATE TABLE user_identities (
  provider   text NOT NULL,
  subject    text NOT NULL,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, subject)
);

-- "Which directories is this person linked to" — asked by the account screen
-- and by an admin looking at one user, both of which start from the user id.
CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);

GRANT SELECT, INSERT, DELETE ON user_identities TO cc_app;

-- ---------------------------------------------------------------------------
-- Email is matched case-insensitively
-- ---------------------------------------------------------------------------

-- users.email has been UNIQUE and exact-match since migrations/000001, which
-- was invisible while the only thing that typed an email was an operator
-- running `ccd user create`. A sign-in form makes it a defect: someone whose
-- row says filip@example.com and who types Filip@example.com is simply told
-- their credentials are wrong, and no amount of correct password helps.
--
-- A functional unique index rather than citext: it needs no extension, and it
-- states the rule where the rule belongs. The original UNIQUE (email) stays —
-- it is not wrong, only insufficient.
--
-- If this index fails to create, the installation already has two accounts
-- whose emails differ only in case. That is worth failing the migration for:
-- silently merging them is a decision about who may read whose projects, and
-- it is not one a migration gets to make.
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));
