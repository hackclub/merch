-- +goose Up
-- When a request shipped, and what the warehouse (mail.hackclub.com) charged
-- for it in cents: contents and labor when it takes the order, postage once
-- it's mailed. NULL until the warehouse says. The admin dashboard adds these
-- up by day, falling back to estimates.
ALTER TABLE swag_requests
    ADD COLUMN dispatched_at          TIMESTAMPTZ,
    ADD COLUMN warehouse_contents_cents INT,
    ADD COLUMN warehouse_labor_cents  INT,
    ADD COLUMN warehouse_postage_cents INT;

-- Requests shipped from the app logged request_dispatched; imports have mailed_at.
UPDATE swag_requests r SET dispatched_at = e.at
FROM (SELECT (props->>'request_id')::bigint AS id, min(at) AS at FROM usage_events
      WHERE name = 'request_dispatched' AND props->>'request_id' ~ '^[0-9]+$' GROUP BY 1) e
WHERE r.id = e.id AND r.status = 'dispatched';
UPDATE swag_requests SET dispatched_at = coalesce(mailed_at, updated_at)
WHERE status = 'dispatched' AND dispatched_at IS NULL;

-- +goose Down
ALTER TABLE swag_requests DROP COLUMN dispatched_at, DROP COLUMN warehouse_contents_cents,
    DROP COLUMN warehouse_labor_cents, DROP COLUMN warehouse_postage_cents;
