-- +goose Up
-- An admin can add an item the requester didn't ask for when shipping:
-- requested_quantity = 0 marks a line that was added.
ALTER TABLE swag_request_items DROP CONSTRAINT swag_request_items_requested_quantity_check;
ALTER TABLE swag_request_items ADD CONSTRAINT swag_request_items_requested_quantity_check CHECK (requested_quantity >= 0);

-- +goose Down
DELETE FROM swag_request_items WHERE requested_quantity = 0;
ALTER TABLE swag_request_items DROP CONSTRAINT swag_request_items_requested_quantity_check;
ALTER TABLE swag_request_items ADD CONSTRAINT swag_request_items_requested_quantity_check CHECK (requested_quantity > 0);
