-- +goose Up
-- An item can name an older item to give out first (the Heidi T-shirt names the
-- Hack Club T-shirt): a size of it can be requested only once the older item is
-- out of that size. Set on the admin Items tab; NULL = none.
ALTER TABLE items ADD COLUMN use_up_first BIGINT REFERENCES items(id) ON DELETE SET NULL
    CONSTRAINT items_use_up_first_not_self CHECK (use_up_first <> id);

-- +goose Down
ALTER TABLE items DROP COLUMN use_up_first;
