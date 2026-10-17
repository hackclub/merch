-- +goose Up
-- No cooldown between requests and no cap on different items per request:
-- each request is reviewed before it ships.
ALTER TABLE settings DROP COLUMN max_items_per_request, DROP COLUMN request_cooldown_days;

-- +goose Down
ALTER TABLE settings
    ADD COLUMN max_items_per_request INT NOT NULL DEFAULT 5 CHECK (max_items_per_request >= 0),
    ADD COLUMN request_cooldown_days INT NOT NULL DEFAULT 30 CHECK (request_cooldown_days >= 0);
