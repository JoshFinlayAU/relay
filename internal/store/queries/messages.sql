-- name: InsertMessage :one
INSERT INTO messages (
    id, direction, credential_id, domain_id, mail_from, header_from, rcpt_to,
    subject, size_bytes, dkim_selector, body_ref, verp_token, status,
    spf_result, dkim_result
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
    sqlc.narg('spf_result'), sqlc.narg('dkim_result')
)
RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = $1;

-- name: ListMessages :many
SELECT * FROM messages
WHERE (sqlc.narg('direction')::text IS NULL OR direction = sqlc.narg('direction'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('domain_id')::uuid IS NULL OR domain_id = sqlc.narg('domain_id'))
  AND (sqlc.narg('credential_id')::uuid IS NULL OR credential_id = sqlc.narg('credential_id'))
  AND (sqlc.narg('after')::timestamptz IS NULL OR created_at >= sqlc.narg('after'))
  AND (sqlc.narg('before')::timestamptz IS NULL OR created_at <= sqlc.narg('before'))
  AND (sqlc.narg('rcpt_like')::text IS NULL
       OR EXISTS (SELECT 1 FROM unnest(rcpt_to) AS r WHERE r ILIKE sqlc.narg('rcpt_like')))
  AND (sqlc.narg('from_like')::text IS NULL OR header_from ILIKE sqlc.narg('from_like'))
  AND (sqlc.narg('subject_like')::text IS NULL OR subject ILIKE sqlc.narg('subject_like'))
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: MessagesOlderThan :many
-- Retention (age mode): message rows + their body refs older than the cutoff.
SELECT id, body_ref FROM messages
WHERE created_at < $1
ORDER BY created_at
LIMIT $2;

-- name: MessagesBeyondCount :many
-- Retention (count mode): rows to delete = everything past the newest `keep`.
SELECT id, body_ref FROM messages
ORDER BY created_at DESC
OFFSET sqlc.arg('keep')
LIMIT sqlc.arg('lim');

-- name: DeleteMessagesByIDs :execrows
DELETE FROM messages WHERE id = ANY(sqlc.arg('ids')::uuid[]);

-- name: MessageStatusCounts :many
SELECT status, count(*)::bigint AS n FROM messages
WHERE direction = 'outbound' AND created_at > $1
GROUP BY status;

-- name: CountDegradedDomains :one
SELECT count(*) FROM domains WHERE status = 'degraded';

-- name: SetMessageStatus :exec
UPDATE messages SET status = $2 WHERE id = $1;

-- name: CountRecentMessagesByCredential :one
SELECT count(*) FROM messages
WHERE credential_id = $1 AND created_at > $2;

-- name: EnqueueDeliveryJob :one
INSERT INTO delivery_jobs (message_id, rcpt)
VALUES ($1, $2)
RETURNING *;
