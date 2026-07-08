-- name: CredentialStats :one
-- Windowed counts for a credential (since a cutoff timestamp).
WITH msgs AS (
    SELECT id FROM messages m WHERE m.credential_id = $1 AND m.created_at > $2
)
SELECT
    (SELECT count(*) FROM msgs)::bigint AS submitted,
    (SELECT count(DISTINCT (da.message_id, da.rcpt)) FROM delivery_attempts da
       WHERE da.message_id IN (SELECT id FROM msgs) AND da.result = 'delivered')::bigint AS delivered,
    (SELECT count(*) FROM delivery_jobs dj
       WHERE dj.message_id IN (SELECT id FROM msgs) AND dj.status = 'deferred')::bigint AS deferred,
    (SELECT count(*) FROM bounce_events be
       WHERE be.message_id IN (SELECT id FROM msgs) AND be.type = 'hard')::bigint AS bounced_hard,
    (SELECT count(*) FROM bounce_events be
       WHERE be.message_id IN (SELECT id FROM msgs) AND be.type = 'soft')::bigint AS bounced_soft,
    (SELECT count(*) FROM bounce_events be
       WHERE be.message_id IN (SELECT id FROM msgs) AND be.type = 'complaint')::bigint AS complaints;

-- name: DomainStats :one
WITH msgs AS (
    SELECT id FROM messages m WHERE m.domain_id = $1 AND m.direction = 'outbound' AND m.created_at > $2
)
SELECT
    (SELECT count(*) FROM msgs)::bigint AS submitted,
    (SELECT count(DISTINCT (da.message_id, da.rcpt)) FROM delivery_attempts da
       WHERE da.message_id IN (SELECT id FROM msgs) AND da.result = 'delivered')::bigint AS delivered,
    (SELECT count(*) FROM delivery_jobs dj
       WHERE dj.message_id IN (SELECT id FROM msgs) AND dj.status = 'deferred')::bigint AS deferred,
    (SELECT count(*) FROM bounce_events be
       WHERE be.message_id IN (SELECT id FROM msgs) AND be.type = 'hard')::bigint AS bounced_hard,
    (SELECT count(*) FROM bounce_events be
       WHERE be.message_id IN (SELECT id FROM msgs) AND be.type = 'soft')::bigint AS bounced_soft,
    (SELECT count(*) FROM bounce_events be
       WHERE be.message_id IN (SELECT id FROM msgs) AND be.type = 'complaint')::bigint AS complaints;

-- name: UpsertStatRollup :exec
-- Recompute one hourly per-domain bucket from source tables.
INSERT INTO stat_rollups (bucket, domain_id, submitted, delivered, deferred, bounced_hard, bounced_soft, complaints)
SELECT
    sqlc.arg('bucket')::timestamptz AS bucket,
    sqlc.arg('domain_id')::uuid AS domain_id,
    (SELECT count(*) FROM messages m WHERE m.domain_id = sqlc.arg('domain_id') AND m.direction = 'outbound'
        AND m.created_at >= sqlc.arg('bucket') AND m.created_at < sqlc.arg('bucket') + interval '1 hour'),
    (SELECT count(DISTINCT (da.message_id, da.rcpt)) FROM delivery_attempts da
        JOIN messages m ON m.id = da.message_id
        WHERE m.domain_id = sqlc.arg('domain_id') AND da.result = 'delivered'
        AND da.started_at >= sqlc.arg('bucket') AND da.started_at < sqlc.arg('bucket') + interval '1 hour'),
    (SELECT count(*) FROM delivery_attempts da
        JOIN messages m ON m.id = da.message_id
        WHERE m.domain_id = sqlc.arg('domain_id') AND da.result = 'deferred'
        AND da.started_at >= sqlc.arg('bucket') AND da.started_at < sqlc.arg('bucket') + interval '1 hour'),
    (SELECT count(*) FROM bounce_events be
        JOIN messages m ON m.id = be.message_id
        WHERE m.domain_id = sqlc.arg('domain_id') AND be.type = 'hard'
        AND be.created_at >= sqlc.arg('bucket') AND be.created_at < sqlc.arg('bucket') + interval '1 hour'),
    (SELECT count(*) FROM bounce_events be
        JOIN messages m ON m.id = be.message_id
        WHERE m.domain_id = sqlc.arg('domain_id') AND be.type = 'soft'
        AND be.created_at >= sqlc.arg('bucket') AND be.created_at < sqlc.arg('bucket') + interval '1 hour'),
    (SELECT count(*) FROM bounce_events be
        JOIN messages m ON m.id = be.message_id
        WHERE m.domain_id = sqlc.arg('domain_id') AND be.type = 'complaint'
        AND be.created_at >= sqlc.arg('bucket') AND be.created_at < sqlc.arg('bucket') + interval '1 hour')
ON CONFLICT (bucket, domain_id, credential_id) DO UPDATE SET
    submitted = EXCLUDED.submitted, delivered = EXCLUDED.delivered, deferred = EXCLUDED.deferred,
    bounced_hard = EXCLUDED.bounced_hard, bounced_soft = EXCLUDED.bounced_soft, complaints = EXCLUDED.complaints;

-- name: ListStatRollups :many
SELECT bucket, submitted, delivered, deferred, bounced_hard, bounced_soft, complaints
FROM stat_rollups
WHERE domain_id = $1 AND credential_id IS NULL AND bucket >= $2
ORDER BY bucket;

-- name: DistinctActiveDomainIDs :many
SELECT DISTINCT domain_id FROM messages WHERE domain_id IS NOT NULL AND created_at > $1;
