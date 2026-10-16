-- +goose Up

-- Roles. The column defaults to 'member' and the registration path never
-- accepts a role, so there is no self-promotion through the API.
--
-- The first admin must be created by hand:
--
--   UPDATE users SET role = 'admin' WHERE email = 'you@example.com';
--
-- After that, an admin can promote others via PATCH /users/{id}/role.

ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'member';

ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('member', 'admin'));

CREATE INDEX idx_users_role ON users(role);

-- +goose Down

DROP INDEX idx_users_role;

ALTER TABLE users DROP CONSTRAINT users_role_check;

ALTER TABLE users DROP COLUMN role;
