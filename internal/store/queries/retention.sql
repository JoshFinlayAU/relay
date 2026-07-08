-- Retention: find/clear stored bodies past their per-direction TTL and prune
-- old metadata rows. Bodies are content-addressed, so callers must confirm no
-- other message still references a body_ref before deleting the blob file.

-- name: ExpiredOutboundBodies :many
-- Outbound messages whose stored body is older than the cutoff.
SELECT id, body_ref FROM messages
WHERE direction = 'outbound' AND body_ref IS NOT NULL AND created_at < $1
ORDER BY created_at
LIMIT $2;

-- name: ExpiredInboundBodies :many
-- Inbound messages older than the cutoff whose body is no longer needed: no
-- webhook delivery is still pending (all succeeded, dead-lettered, or none).
SELECT m.id, m.body_ref FROM messages m
WHERE m.direction = 'inbound' AND m.body_ref IS NOT NULL AND m.created_at < $1
  AND NOT EXISTS (
    SELECT 1 FROM webhook_deliveries wd
    WHERE wd.message_id = m.id AND wd.result = 'pending'
  )
ORDER BY m.created_at
LIMIT $2;

-- name: ClearMessageBodyRef :exec
UPDATE messages SET body_ref = NULL WHERE id = $1;

-- name: CountBodyRefUsers :one
-- How many messages still reference a given body_ref (guards blob deletion).
SELECT count(*) FROM messages WHERE body_ref = $1;

-- name: DeleteOldMessages :execrows
-- Prune message metadata past the retention window. FK cascades remove the
-- associated delivery_jobs/attempts/bounce_events; stats rollups are separate.
DELETE FROM messages WHERE created_at < $1;

-- name: DeleteOldEvents :execrows
DELETE FROM events WHERE created_at < $1;
