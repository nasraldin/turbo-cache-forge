-- +goose Up
-- Read-only cache tokens: a token so flagged may pull (GET/HEAD/status/batch)
-- but never PUT. Enforced on the cache path in internal/turbo; the /api/v1
-- (human/JWT) world is unaffected — a cache token never reaches it.
ALTER TABLE api_keys ADD COLUMN read_only BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN read_only;
