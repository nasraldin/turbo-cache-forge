-- +goose Up
ALTER TABLE api_keys ADD COLUMN read_only INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN read_only;
