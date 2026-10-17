-- +goose Up
-- Every email the app sends (or tries to), word for word, so audits can read
-- what people were told. usage_events' email_sent and email_failed carry the
-- row's id as email_id.
CREATE TABLE sent_emails (
    id              BIGSERIAL PRIMARY KEY,
    at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind            TEXT NOT NULL,
    request_id      BIGINT REFERENCES swag_requests (id) ON DELETE SET NULL,
    user_id         BIGINT REFERENCES users (id) ON DELETE SET NULL,
    to_email        TEXT NOT NULL,
    subject         TEXT NOT NULL,
    preview         TEXT NOT NULL,
    body            TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'unsent')),
    error           TEXT NOT NULL DEFAULT '',
    release         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sent_emails_at ON sent_emails (at DESC);
CREATE INDEX sent_emails_request ON sent_emails (request_id);

-- +goose Down
DROP TABLE sent_emails;
