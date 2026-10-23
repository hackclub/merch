-- +goose Up
-- Staff asking for postcards or posters to be printed (Vistaprint) and stocked
-- in the warehouse: the product (swag.PrintProducts), their print-ready art on
-- the CDN, and what we estimated it would cost when they asked.
CREATE TABLE print_requests (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users (id),
    product        TEXT NOT NULL,
    title          TEXT NOT NULL,
    quantity       INT NOT NULL CHECK (quantity > 0),
    front_url      TEXT NOT NULL,
    back_url       TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT '',
    needed_by      DATE,
    estimate_cents INT NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'ordered', 'stocked', 'rejected')),
    admin_note     TEXT NOT NULL DEFAULT '',
    sku            TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX print_requests_user_idx ON print_requests (user_id, created_at DESC);

-- +goose Down
DROP TABLE print_requests;
