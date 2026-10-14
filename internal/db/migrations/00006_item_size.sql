-- +goose Up
-- Physical size of printed items, in inches (e.g. 11 x 17 for a poster). Drives
-- the true-to-scale 3D preview on product pages; NULL when it doesn't apply.
ALTER TABLE items ADD COLUMN width_in DOUBLE PRECISION, ADD COLUMN height_in DOUBLE PRECISION;

-- +goose Down
ALTER TABLE items DROP COLUMN width_in, DROP COLUMN height_in;
