-- +goose Up
-- Items can come in different packs: postcards in 5, 10, 15, 20, 25, stickers
-- in 3, 10, then both in multiples of 50. See swag.Packs. '' = any quantity.
ALTER TABLE items ADD COLUMN packs TEXT NOT NULL DEFAULT '';
UPDATE items SET packs = 'postcard' WHERE sold_in_packs;
ALTER TABLE items DROP COLUMN sold_in_packs;

-- +goose Down
ALTER TABLE items ADD COLUMN sold_in_packs BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE items SET sold_in_packs = packs <> '';
ALTER TABLE items DROP COLUMN packs;
