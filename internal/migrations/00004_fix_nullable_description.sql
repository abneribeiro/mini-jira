-- +goose Up

-- The Go models scan `description` into a plain string, which fails with
-- "cannot scan NULL into *string" for any row written outside this API.
-- The INSERT path already stores '' rather than NULL, so make the schema
-- match the only value the application can actually produce.

UPDATE projects SET description = '' WHERE description IS NULL;

ALTER TABLE projects
    ALTER COLUMN description SET DEFAULT '',
    ALTER COLUMN description SET NOT NULL;

UPDATE tasks SET description = '' WHERE description IS NULL;

ALTER TABLE tasks
    ALTER COLUMN description SET DEFAULT '',
    ALTER COLUMN description SET NOT NULL;

-- Supports the tasks_by_status gauge and status filtering.
CREATE INDEX idx_tasks_status ON tasks(status);

-- +goose Down

DROP INDEX idx_tasks_status;

ALTER TABLE tasks
    ALTER COLUMN description DROP NOT NULL,
    ALTER COLUMN description DROP DEFAULT;

ALTER TABLE projects
    ALTER COLUMN description DROP NOT NULL,
    ALTER COLUMN description DROP DEFAULT;
