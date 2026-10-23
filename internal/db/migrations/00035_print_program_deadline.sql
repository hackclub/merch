-- +goose Up
-- What a print run is for: the program's name (title is what the item is
-- called in the warehouse and the shop), and its final deadline in the
-- requester's words, e.g. when an in-person event happens. needed_by stays
-- for older requests; the form no longer asks for a date.
ALTER TABLE print_requests
    ADD COLUMN program TEXT NOT NULL DEFAULT '',
    ADD COLUMN deadline TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE print_requests DROP COLUMN program, DROP COLUMN deadline;
