-- +goose Up
-- The original print files of an item (postcard and poster artwork), so people
-- can print their own: [{url, label, bytes}].
ALTER TABLE items ADD COLUMN print_files JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE items DROP COLUMN print_files;
