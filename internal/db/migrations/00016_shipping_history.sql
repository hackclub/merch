-- +goose Up
-- One unit's shipping weight in ounces, for packing requests and the 3 lb
-- international limit. NULL: worked out from a printed item's size.
ALTER TABLE items ADD COLUMN weight_oz DOUBLE PRECISION CHECK (weight_oz IS NULL OR weight_oz > 0);

-- Packages mailed before, for shipping estimates (internal/swag/estimate.go).
-- Loaded with cmd/shiphistory from the warehouse's records. No names or
-- addresses: just where each went, how it went, and when.
CREATE TABLE shipping_history (
    id            BIGSERIAL PRIMARY KEY,
    source        TEXT NOT NULL,                 -- zenventory, theseus_letter
    country       TEXT NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    class         TEXT NOT NULL CHECK (class IN ('letter', 'parcel')),
    weight_oz     DOUBLE PRECISION,
    postage_cents INT,
    ordered_on    DATE,
    shipped_on    DATE NOT NULL,
    delivered_on  DATE
);

-- +goose Down
DROP TABLE shipping_history;
ALTER TABLE items DROP COLUMN weight_oz;
