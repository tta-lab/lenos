-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN total_prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (total_prompt_tokens >= 0);
ALTER TABLE sessions ADD COLUMN total_completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (total_completion_tokens >= 0);
ALTER TABLE sessions ADD COLUMN total_reasoning_tokens INTEGER NOT NULL DEFAULT 0 CHECK (total_reasoning_tokens >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN total_reasoning_tokens;
ALTER TABLE sessions DROP COLUMN total_completion_tokens;
ALTER TABLE sessions DROP COLUMN total_prompt_tokens;
-- +goose StatementEnd
