-- +goose Up
-- Requests stop at what the warehouse has, except for items that can be
-- backordered: they ship when the restock arrives. The Haven postcard and
-- poster, which have big reprints on the way, start that way.
ALTER TABLE items ADD COLUMN allow_backorder BOOLEAN NOT NULL DEFAULT false;
UPDATE items SET allow_backorder = true WHERE sku IN ('Pri/Hav/4x6/1st', 'Pri/Hav/11x/1st');

-- +goose Down
ALTER TABLE items DROP COLUMN allow_backorder;
