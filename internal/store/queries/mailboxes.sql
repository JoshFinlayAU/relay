-- name: CreateMailbox :one
INSERT INTO mailboxes (domain_id, local_part, webhook_url, webhook_secret_enc)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMailbox :one
SELECT * FROM mailboxes WHERE id = $1;

-- name: ListMailboxesByDomain :many
SELECT * FROM mailboxes WHERE domain_id = $1 ORDER BY local_part;

-- name: FindMailbox :one
-- Match an exact local part, falling back to a catch-all ('*'), among active mailboxes.
SELECT * FROM mailboxes
WHERE domain_id = $1 AND status = 'active' AND local_part IN (lower(sqlc.arg(local_part)), '*')
ORDER BY (local_part = '*') ASC
LIMIT 1;

-- name: DeleteMailbox :exec
DELETE FROM mailboxes WHERE id = $1;

-- name: SetMailboxStatus :one
UPDATE mailboxes SET status = $2 WHERE id = $1 RETURNING *;

-- name: UpdateMailboxWebhook :one
UPDATE mailboxes SET webhook_url = $2, webhook_secret_enc = $3 WHERE id = $1 RETURNING *;
