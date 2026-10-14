-- +goose Up
-- Items can come in sizes (e.g. a T-shirt in S/M/L/XL), each its own
-- warehouse SKU: [{"label": "M", "sku": "Swa/Shirt/HC/M"}, ...].
ALTER TABLE items ADD COLUMN variants JSONB NOT NULL DEFAULT '[]';

-- Request lines record the exact SKU shipped (the chosen size, or the item's
-- SKU) so later item edits don't rewrite history. One request can hold several
-- sizes of an item once per-size items are folded into one.
ALTER TABLE swag_request_items
    ADD COLUMN sku     TEXT NOT NULL DEFAULT '',
    ADD COLUMN variant TEXT NOT NULL DEFAULT '';
UPDATE swag_request_items ri SET sku = i.sku FROM items i WHERE i.id = ri.item_id;
ALTER TABLE swag_request_items DROP CONSTRAINT swag_request_items_pkey;
ALTER TABLE swag_request_items ADD PRIMARY KEY (request_id, item_id, sku);

-- +goose Down
ALTER TABLE swag_request_items DROP CONSTRAINT swag_request_items_pkey;
ALTER TABLE swag_request_items ADD PRIMARY KEY (request_id, item_id);
ALTER TABLE swag_request_items DROP COLUMN variant, DROP COLUMN sku;
ALTER TABLE items DROP COLUMN variants;
