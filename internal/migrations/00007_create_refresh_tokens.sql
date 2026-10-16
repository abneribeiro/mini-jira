-- +goose Up

-- Refresh tokens make revocation possible: the access token stays stateless
-- and short-lived, while the long-lived credential lives here and can be
-- revoked.
--
-- Only the SHA-256 hash is stored, so a database leak does not hand out
-- usable tokens. family_id groups a rotation chain: presenting an already
-- revoked token means it leaked, and the whole family is revoked at once.

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY,

    user_id UUID NOT NULL
        REFERENCES users(id)
        ON DELETE CASCADE,

    family_id UUID NOT NULL,

    token_hash BYTEA NOT NULL UNIQUE,

    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_family_id ON refresh_tokens(family_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

-- +goose Down

DROP TABLE refresh_tokens;
