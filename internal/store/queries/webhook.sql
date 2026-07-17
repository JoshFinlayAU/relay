-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (mailbox_id, message_id, next_attempt_at, result)
VALUES ($1, $2, now(), 'pending')
RETURNING *;

-- name: ClaimWebhookDeliveries :many
UPDATE webhook_deliveries SET attempt_no = attempt_no + 1
WHERE id IN (
    SELECT id FROM webhook_deliveries
    WHERE result = 'pending' AND next_attempt_at <= now()
    ORDER BY next_attempt_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkWebhookSuccess :exec
UPDATE webhook_deliveries SET result = 'success', status_code = $2, response_snippet = $3 WHERE id = $1;

-- name: MarkWebhookRetry :exec
UPDATE webhook_deliveries SET result = 'pending', status_code = $2, response_snippet = $3, next_attempt_at = $4 WHERE id = $1;

-- name: MarkWebhookDeadLetter :exec
UPDATE webhook_deliveries SET result = 'dead_letter', status_code = $2, response_snippet = $3 WHERE id = $1;

-- name: RequeueWebhookDelivery :exec
UPDATE webhook_deliveries SET result = 'pending', next_attempt_at = now(), status_code = NULL, response_snippet = NULL WHERE id = $1;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE id = $1;

-- name: ListWebhookDeliveriesByDomain :many
SELECT wd.* FROM webhook_deliveries wd
JOIN mailboxes m ON m.id = wd.mailbox_id
WHERE m.domain_id = $1
ORDER BY wd.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListWebhookDeliveriesByMessage :many
SELECT * FROM webhook_deliveries WHERE message_id = $1 ORDER BY attempt_no;
