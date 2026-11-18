-- A password an administrator set is temporary: its owner has to replace it
-- before using the interface for anything else (docs/adr/0041). Their own
-- change clears it.
ALTER TABLE user_passwords ADD COLUMN must_change boolean NOT NULL DEFAULT false;
