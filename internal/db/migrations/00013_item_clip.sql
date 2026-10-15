-- +goose Up
-- A short looping video (or GIF) of the real item, like someone flipping
-- through a magazine; the shop tile plays it instead of the 3D view.
ALTER TABLE items ADD COLUMN clip TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE items DROP COLUMN clip;
