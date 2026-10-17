-- +goose Up
-- An admin can change what ships: quantity is what ships (0 = removed), and
-- requested_quantity keeps what was asked for when it differs (NULL = as asked).
ALTER TABLE swag_request_items DROP CONSTRAINT swag_request_items_quantity_check;
ALTER TABLE swag_request_items ADD CONSTRAINT swag_request_items_quantity_check CHECK (quantity >= 0);
ALTER TABLE swag_request_items ADD COLUMN requested_quantity INT CHECK (requested_quantity > 0);

-- +goose Down
DELETE FROM swag_request_items WHERE quantity = 0;
ALTER TABLE swag_request_items DROP COLUMN requested_quantity;
ALTER TABLE swag_request_items DROP CONSTRAINT swag_request_items_quantity_check;
ALTER TABLE swag_request_items ADD CONSTRAINT swag_request_items_quantity_check CHECK (quantity > 0);
