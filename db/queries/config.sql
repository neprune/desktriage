-- name: GetConfig :one
SELECT * FROM config WHERE key = ?;

-- name: ListConfig :many
SELECT * FROM config ORDER BY key;

-- name: UpsertConfig :exec
INSERT INTO config (key, value, description, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value,
    description = excluded.description,
    updated_at = excluded.updated_at;

-- name: DeleteConfig :exec
DELETE FROM config WHERE key = ?;
