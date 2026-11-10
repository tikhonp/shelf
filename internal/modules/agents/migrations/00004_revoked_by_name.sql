-- +goose Up
ALTER TABLE agents_connections ADD COLUMN revoked_by_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agents_connections DROP COLUMN revoked_by_name;
