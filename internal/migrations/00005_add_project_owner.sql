-- +goose Up

-- Projects had no owner, so any authenticated user could read, modify and
-- delete every project (and, by cascade, every task). Ownership is the column
-- the authorization checks key on.

ALTER TABLE projects
    ADD COLUMN owner_id UUID REFERENCES users(id) ON DELETE CASCADE;

-- Backfill so the column can be NOT NULL. Existing rows predate ownership,
-- so they go to the earliest account.
UPDATE projects
SET owner_id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
WHERE owner_id IS NULL;

-- Only reachable when the projects table has rows but the users table is
-- empty; such a project can never be owned or accessed.
DELETE FROM projects WHERE owner_id IS NULL;

ALTER TABLE projects
    ALTER COLUMN owner_id SET NOT NULL;

CREATE INDEX idx_projects_owner_id ON projects(owner_id);

-- +goose Down

DROP INDEX idx_projects_owner_id;

ALTER TABLE projects DROP COLUMN owner_id;
