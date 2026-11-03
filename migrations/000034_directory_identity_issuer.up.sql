-- The issuer beside each directory link — docs/adr/0023.
--
-- user_identities is keyed on (provider, subject), and provider is the
-- operator's --oidc-name, not anything the provider says about itself. So
-- re-pointing --oidc-issuer at a different issuer while keeping the name let
-- the new issuer's subjects resolve to the old one's links: a `sub` is only
-- unique within the issuer that minted it, and two issuers can mint the same
-- string for two different people.
--
-- NULL means "no issuer recorded", and covers two kinds of row:
--   * LDAP links, which have no issuer and keep NULL for good;
--   * OIDC links made before this migration. The configured issuer is not in
--     the database, so nothing here can backfill them. The first OIDC
--     sign-in through such a link records the issuer configured at that
--     moment (identity.ProvisionDirectoryUserWith); from then on a different
--     issuer is refused.
ALTER TABLE user_identities ADD COLUMN issuer text;

-- Only the issuer, and only so a legacy row can adopt one: the link itself
-- (provider, subject, user_id) stays insert-or-delete, never re-pointed.
GRANT UPDATE (issuer) ON user_identities TO cc_app;
