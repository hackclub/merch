-- +goose Up
-- A 3D model (GLB URL) of an item that isn't printed, like the Orpheus
-- plushie: the product page and shop tile turn it in 3D.
ALTER TABLE items ADD COLUMN model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE items DROP COLUMN model;
