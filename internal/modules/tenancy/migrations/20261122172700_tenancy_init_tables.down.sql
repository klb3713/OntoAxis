-- Manual rollback script for 20261122172700_tenancy_init_tables.sql.
-- The goose Down section in the Up migration contains the same statements.
-- Run only with an explicit rollback decision; this drops all tenancy tables.

DROP TABLE IF EXISTS core.project_resources;
DROP TABLE IF EXISTS core.projects;
DROP TABLE IF EXISTS core.memberships;
DROP TABLE IF EXISTS core.workspaces;
DROP TABLE IF EXISTS core.organizations;
