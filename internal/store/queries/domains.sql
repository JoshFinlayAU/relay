-- name: CountDomains :one
SELECT count(*) FROM domains;

-- name: GetDomainByName :one
SELECT * FROM domains WHERE name = $1;

-- name: GetDomain :one
SELECT * FROM domains WHERE id = $1;

-- name: CreateDomain :one
INSERT INTO domains (name, receiving, verify_token, bounce_subdomain)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListDomains :many
SELECT * FROM domains
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListActiveDomains :many
SELECT * FROM domains WHERE status IN ('active','degraded');

-- name: UpdateDomainStatus :one
UPDATE domains SET status = $2 WHERE id = $1 RETURNING *;

-- name: SetDomainReceiving :one
UPDATE domains SET receiving = $2 WHERE id = $1 RETURNING *;

-- name: SetDomainSendingPaused :one
UPDATE domains SET sending_paused = $2 WHERE id = $1 RETURNING *;

-- name: SetDomainForwardBounces :one
UPDATE domains SET forward_bounces = $2 WHERE id = $1 RETURNING *;

-- name: SetDomainDeliveryMaxAge :one
-- $2 NULL clears the override (fall back to the global default).
UPDATE domains SET delivery_max_age_seconds = $2 WHERE id = $1 RETURNING *;

-- name: GetDomainDeliveryMaxAge :one
SELECT delivery_max_age_seconds FROM domains WHERE id = $1;

-- name: DeleteDomain :exec
DELETE FROM domains WHERE id = $1;
