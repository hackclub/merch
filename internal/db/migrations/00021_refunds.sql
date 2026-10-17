-- +goose Up
-- Shipping payments given back: all of it when a paid request is rejected or
-- cancelled, or the postage it no longer needed when an admin shipped less.
ALTER TABLE swag_requests
    ADD COLUMN refunded_at    TIMESTAMPTZ,
    ADD COLUMN refunded_cents INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE swag_requests DROP COLUMN refunded_at, DROP COLUMN refunded_cents;
