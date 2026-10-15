-- +goose Up
-- The amounts offered as one-tap buttons after "Add" on the shop (e.g. 1, 3, 5,
-- 10 for the full-size magazine). Empty: worked out from the item's limits.
ALTER TABLE items ADD COLUMN quick_picks INT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE items DROP COLUMN quick_picks;
