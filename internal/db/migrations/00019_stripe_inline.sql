-- +goose Up
-- Card payments moved inline (Payment Element + PaymentIntent), so there are no
-- Checkout pages to remember; stripe_payment_intent holds the request's payment.
ALTER TABLE swag_requests DROP COLUMN stripe_session_id;

-- +goose Down
ALTER TABLE swag_requests ADD COLUMN stripe_session_id TEXT NOT NULL DEFAULT '';
