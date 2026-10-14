-- +goose Up
-- Items requested in packs (postcards): quantities 5, 10, 15, 20, 25, then
-- multiples of 50. See swag.PackQuantity.
ALTER TABLE items ADD COLUMN sold_in_packs BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE items DROP COLUMN sold_in_packs;
