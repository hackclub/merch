-- +goose Up
-- Items get an ordered gallery (e.g. front and back of a poster) instead of a
-- single image. URLs point at cdn.hackclub.com; the first one is the thumbnail.
ALTER TABLE items ADD COLUMN images TEXT[] NOT NULL DEFAULT '{}';
UPDATE items SET images = ARRAY[image_url] WHERE image_url <> '';
ALTER TABLE items DROP COLUMN image_url;

-- +goose Down
ALTER TABLE items ADD COLUMN image_url TEXT NOT NULL DEFAULT '';
UPDATE items SET image_url = images[1] WHERE cardinality(images) > 0;
ALTER TABLE items DROP COLUMN images;
