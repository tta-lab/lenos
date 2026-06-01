-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN cache_miss_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_miss_tokens >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN cache_miss_tokens;
-- +goose StatementEnd
