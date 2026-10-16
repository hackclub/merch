-- +goose Up
-- International shipping paid by card through Stripe Checkout: the latest
-- payment page opened for the request, and the payment that paid it.
ALTER TABLE swag_requests
    ADD COLUMN stripe_session_id     TEXT NOT NULL DEFAULT '',
    ADD COLUMN stripe_payment_intent TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE swag_requests DROP COLUMN stripe_session_id, DROP COLUMN stripe_payment_intent;
