-- name: GetCache :one
SELECT * FROM api_cache WHERE cache_key = ?;

-- name: SetCache :exec
INSERT INTO api_cache (cache_key, response_body, etag, cached_at, max_age_secs)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
    response_body = excluded.response_body,
    etag = excluded.etag,
    cached_at = excluded.cached_at,
    max_age_secs = excluded.max_age_secs;

-- name: DeleteCache :exec
DELETE FROM api_cache WHERE cache_key = ?;

-- name: DeleteExpiredCache :exec
DELETE FROM api_cache WHERE datetime(cached_at, '+' || max_age_secs || ' seconds') < datetime('now');

-- name: DeleteAllCache :exec
DELETE FROM api_cache;
