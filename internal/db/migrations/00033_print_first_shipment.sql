-- +goose Up
-- A print run's first shipment to the staff member who asked for it, so they
-- can see it in person: how many and where. When the run is stocked, it
-- becomes a swag request (shipment_request_id). The preview URLs are the art
-- trimmed and shrunk for the shop item the run is listed as, which is landscape
-- when the art is.
ALTER TABLE print_requests
    ADD COLUMN initial_quantity INT NOT NULL DEFAULT 0 CHECK (initial_quantity >= 0),
    ADD COLUMN address JSONB,
    ADD COLUMN shipment_request_id BIGINT REFERENCES swag_requests (id),
    ADD COLUMN front_preview_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN back_preview_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN landscape BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE print_requests
    DROP COLUMN initial_quantity, DROP COLUMN address, DROP COLUMN shipment_request_id,
    DROP COLUMN front_preview_url, DROP COLUMN back_preview_url, DROP COLUMN landscape;
