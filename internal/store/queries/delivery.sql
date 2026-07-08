-- name: ClaimDeliveryJobs :many
-- Atomically claim due jobs with SKIP LOCKED so multiple workers never grab the
-- same job. Increments attempts and marks in_progress.
UPDATE delivery_jobs SET
    status = 'in_progress',
    locked_at = now(),
    locked_by = $1,
    attempts = attempts + 1
WHERE id IN (
    SELECT id FROM delivery_jobs
    WHERE status IN ('queued', 'deferred') AND next_attempt_at <= now()
    ORDER BY next_attempt_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkJobDelivered :exec
UPDATE delivery_jobs SET status = 'delivered', last_code = $2, last_response = $3, locked_by = NULL WHERE id = $1;

-- name: DeferJob :exec
UPDATE delivery_jobs SET status = 'deferred', next_attempt_at = $2, last_code = $3, last_response = $4, locked_by = NULL WHERE id = $1;

-- name: FailJob :exec
UPDATE delivery_jobs SET status = 'failed', last_code = $2, last_response = $3, locked_by = NULL WHERE id = $1;

-- name: RequeueStaleJobs :exec
-- Recover jobs whose worker died mid-flight (locked but never completed).
UPDATE delivery_jobs SET status = 'queued', locked_by = NULL, locked_at = NULL
WHERE status = 'in_progress' AND locked_at < $1;

-- name: InsertDeliveryAttempt :exec
INSERT INTO delivery_attempts (
    message_id, rcpt, mx_host, finished_at, result, smtp_code, smtp_response, tls_version, tls_verified
) VALUES ($1, $2, $3, now(), $4, $5, $6, $7, $8);

-- name: JobStatusCounts :one
SELECT
    count(*) FILTER (WHERE status = 'delivered')                              AS delivered,
    count(*) FILTER (WHERE status = 'failed')                                 AS failed,
    count(*) FILTER (WHERE status IN ('queued', 'in_progress', 'deferred'))   AS pending
FROM delivery_jobs WHERE message_id = $1;

-- name: ListDeliveryAttempts :many
SELECT * FROM delivery_attempts WHERE message_id = $1 ORDER BY started_at;

-- name: QueueDepth :one
SELECT count(*) FROM delivery_jobs WHERE status IN ('queued', 'deferred');
