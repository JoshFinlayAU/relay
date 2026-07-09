-- name: CreateAPIToken :one
INSERT INTO api_tokens (name, token_hash) VALUES ($1, $2) RETURNING *;

-- name: ListAPITokens :many
SELECT id, name, created_at, last_used, revoked_at
FROM api_tokens
ORDER BY revoked_at IS NOT NULL, created_at DESC;

-- name: GetActiveAPITokenByHash :one
SELECT * FROM api_tokens WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAPIToken :execrows
UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used = now() WHERE id = $1;
