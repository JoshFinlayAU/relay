-- name: InsertDKIMKey :one
INSERT INTO dkim_keys (domain_id, selector, algorithm, private_key_enc, public_key)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetActiveDKIMKey :one
SELECT * FROM dkim_keys
WHERE domain_id = $1 AND active = true
ORDER BY created_at DESC
LIMIT 1;

-- name: ListDKIMKeys :many
SELECT id, domain_id, selector, algorithm, public_key, active, created_at
FROM dkim_keys WHERE domain_id = $1 ORDER BY created_at DESC;
