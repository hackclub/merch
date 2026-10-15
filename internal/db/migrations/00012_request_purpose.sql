-- +goose Up
-- The request form asks what a request is for: 'personal' (for themselves and
-- friends), 'club' (they run a Hack Club) or 'event'. Imported history has ''.
ALTER TABLE swag_requests ADD COLUMN purpose TEXT NOT NULL DEFAULT '';

-- Club-only items aren't offered for personal requests. The full-size
-- magazine and the T-shirt start that way; admins change it per item.
ALTER TABLE items ADD COLUMN club_only BOOLEAN NOT NULL DEFAULT false;
UPDATE items SET club_only = true WHERE sku IN ('Pri/Bok/M25/1st', 'Swa/Shirt/HC');

-- +goose Down
ALTER TABLE items DROP COLUMN club_only;
ALTER TABLE swag_requests DROP COLUMN purpose;
