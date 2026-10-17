-- +goose Up

-- Every list endpoint pages with ORDER BY created_at DESC. A composite index
-- lets Postgres walk rows already in that order for one owner or project
-- instead of fetching all of them and sorting before applying LIMIT.

-- Supersedes idx_projects_owner_id: the leading column serves the same
-- equality lookups.
CREATE INDEX idx_projects_owner_created ON projects(owner_id, created_at DESC);
DROP INDEX idx_projects_owner_id;

-- Supersedes idx_tasks_project_id for the same reason.
CREATE INDEX idx_tasks_project_created ON tasks(project_id, created_at DESC);
DROP INDEX idx_tasks_project_id;

CREATE INDEX idx_users_created_at ON users(created_at DESC);

-- +goose Down

DROP INDEX idx_users_created_at;

CREATE INDEX idx_tasks_project_id ON tasks(project_id);
DROP INDEX idx_tasks_project_created;

CREATE INDEX idx_projects_owner_id ON projects(owner_id);
DROP INDEX idx_projects_owner_created;
