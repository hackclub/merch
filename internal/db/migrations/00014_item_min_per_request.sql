-- +goose Up
-- The fewest of an item one request can ask for (all sizes together), like the
-- old Hackpad poster form's "Minimum 2". 1 for most things.
ALTER TABLE items ADD COLUMN min_per_request INT NOT NULL DEFAULT 1 CHECK (min_per_request >= 1);

-- +goose Down
ALTER TABLE items DROP COLUMN min_per_request;
