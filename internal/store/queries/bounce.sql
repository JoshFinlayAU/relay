-- name: InsertBounceEvent :one
INSERT INTO bounce_events (message_id, rcpt, type, dsn_code, raw_ref)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListBounceEvents :many
SELECT * FROM bounce_events WHERE message_id = $1 ORDER BY created_at;

-- name: FailJobByRcpt :exec
UPDATE delivery_jobs SET status = 'failed', last_code = $3, last_response = $4, locked_by = NULL
WHERE message_id = $1 AND rcpt = $2;

-- name: AddSuppression :one
INSERT INTO suppressions (domain_id, address, reason)
VALUES ($1, $2, $3)
ON CONFLICT (domain_id, address) DO UPDATE SET reason = EXCLUDED.reason
RETURNING *;

-- name: IsSuppressed :one
SELECT EXISTS (SELECT 1 FROM suppressions WHERE domain_id = $1 AND lower(address) = lower(sqlc.arg(address))) AS suppressed;

-- name: ListSuppressions :many
SELECT * FROM suppressions WHERE domain_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: RemoveSuppression :exec
DELETE FROM suppressions WHERE domain_id = $1 AND lower(address) = lower(sqlc.arg(address));
