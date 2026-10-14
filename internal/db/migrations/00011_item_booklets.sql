-- +goose Up
-- Booklets (magazines): how they're bound ('' = single sheet, 'staple',
-- 'perfect') and their page images in reading order, for the 3D flip-through.
-- ships_with lists warehouse SKUs packed with every unit (the magazine's card).
ALTER TABLE items
    ADD COLUMN binding TEXT NOT NULL DEFAULT '',
    ADD COLUMN pages TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN ships_with TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE items DROP COLUMN binding, DROP COLUMN pages, DROP COLUMN ships_with;
