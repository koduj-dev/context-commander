-- Removing a project member (internal/members.Service.Remove). The
-- project_members_delete policy has existed since 000004; only the grant was
-- missing, because nothing removed a member before.
GRANT DELETE ON project_members TO cc_app;
