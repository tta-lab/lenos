-- name: CreateSession :one
INSERT INTO sessions (
    id,
    parent_session_id,
    title,
    message_count,
    prompt_tokens,
    completion_tokens,
    cache_creation_tokens,
    cache_read_tokens,
    cache_miss_tokens,
    cost,
    summary_message_id,
    updated_at,
    created_at
) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    null,
    strftime('%s', 'now'),
    strftime('%s', 'now')
) RETURNING id, parent_session_id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at, summary_message_id, cache_creation_tokens, cache_read_tokens, cache_miss_tokens;

-- name: GetSessionByID :one
SELECT id, parent_session_id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at, summary_message_id, cache_creation_tokens, cache_read_tokens, cache_miss_tokens
FROM sessions
WHERE id = ? LIMIT 1;

-- name: GetLastSession :one
SELECT id, parent_session_id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at, summary_message_id, cache_creation_tokens, cache_read_tokens, cache_miss_tokens
FROM sessions
ORDER BY updated_at DESC
LIMIT 1;

-- name: ListSessions :many
SELECT id, parent_session_id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at, summary_message_id, cache_creation_tokens, cache_read_tokens, cache_miss_tokens
FROM sessions
WHERE parent_session_id is NULL
ORDER BY updated_at DESC;

-- name: UpdateSession :one
UPDATE sessions
SET
    title = ?,
    prompt_tokens = ?,
    completion_tokens = ?,
    cache_creation_tokens = ?,
    cache_read_tokens = ?,
    cache_miss_tokens = ?,
    summary_message_id = ?,
    cost = ?
WHERE id = ?
RETURNING id, parent_session_id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at, summary_message_id, cache_creation_tokens, cache_read_tokens, cache_miss_tokens;

-- name: UpdateSessionTitleAndUsage :exec
UPDATE sessions
SET
    title = ?,
    prompt_tokens = prompt_tokens + ?,
    completion_tokens = completion_tokens + ?,
    cache_creation_tokens = cache_creation_tokens + ?,
    cache_read_tokens = cache_read_tokens + ?,
    cache_miss_tokens = cache_miss_tokens + ?,
    cost = cost + ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;


-- name: RenameSession :exec
UPDATE sessions
SET
    title = ?
WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = ?;
