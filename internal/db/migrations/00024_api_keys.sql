-- +goose Up
-- Keys for agents that review requests (/api/agent/*). Each acts for the admin
-- who made it; only a hash of the token is stored.
CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    token_hash   BYTEA NOT NULL UNIQUE,
    created_by   BIGINT NOT NULL REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

-- +goose Down
DROP TABLE api_keys;
