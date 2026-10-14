-- +goose Up
-- How a printed item was made, for the 3D preview's surface and edge (finish:
-- matte, gloss, linen or uncoated; thickness in inches), and real-world photos
-- people posted of it: [{url, caption, credit}].
ALTER TABLE items
    ADD COLUMN finish TEXT NOT NULL DEFAULT '',
    ADD COLUMN thickness_in DOUBLE PRECISION,
    ADD COLUMN photos JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE items DROP COLUMN finish, DROP COLUMN thickness_in, DROP COLUMN photos;
