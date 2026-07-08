-- name: InsertEvent :one
INSERT INTO events (domain_id, credential_id, type, detail)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEvents :many
SELECT * FROM events
WHERE (sqlc.narg('domain_id')::uuid IS NULL OR domain_id = sqlc.narg('domain_id'))
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountEvents :one
SELECT count(*) FROM events;
