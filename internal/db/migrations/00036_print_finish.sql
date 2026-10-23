-- +goose Up
-- The paper a print run was ordered on, from its product's finishes
-- (swag.PrintProduct.Finishes): posters come matte or glossy. Older requests
-- have '' and use the product's default.
ALTER TABLE print_requests ADD COLUMN finish TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE print_requests DROP COLUMN finish;
