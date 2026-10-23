-- +goose Up
-- Staff-only items (the Orpheus plushie) are offered only to requests that
-- say they're from Hack Club staff, not to clubs, events or personal requests.
ALTER TABLE items ADD COLUMN staff_only BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE items DROP COLUMN staff_only;
