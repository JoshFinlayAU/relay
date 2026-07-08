-- name: CreateCredential :one
INSERT INTO credentials (domain_id, username, secret_hash, restrictions)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetCredential :one
SELECT * FROM credentials WHERE id = $1;

-- name: GetCredentialByUsername :one
SELECT * FROM credentials WHERE username = $1;

-- name: ListCredentialsByDomain :many
SELECT * FROM credentials
WHERE domain_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateCredentialStatus :one
UPDATE credentials SET status = $2 WHERE id = $1 RETURNING *;

-- name: UpdateCredentialRestrictions :one
UPDATE credentials SET restrictions = $2 WHERE id = $1 RETURNING *;

-- name: TouchCredentialLastUsed :exec
UPDATE credentials SET last_used = now(), failed_auth_count = 0, locked_until = NULL
WHERE id = $1;

-- name: RecordFailedAuth :one
UPDATE credentials
SET failed_auth_count = failed_auth_count + 1
WHERE id = $1
RETURNING failed_auth_count;

-- name: SetCredentialLock :exec
UPDATE credentials SET locked_until = $2 WHERE id = $1;

-- name: DeleteCredential :exec
DELETE FROM credentials WHERE id = $1;

-- name: GrantCredentialDomain :exec
INSERT INTO credential_domains (credential_id, domain_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RevokeCredentialDomain :exec
DELETE FROM credential_domains WHERE credential_id = $1 AND domain_id = $2;

-- name: ListCredentialDomains :many
SELECT d.* FROM domains d
JOIN credential_domains cd ON cd.domain_id = d.id
WHERE cd.credential_id = $1;

-- name: CredentialCoversDomain :one
SELECT (
    EXISTS (SELECT 1 FROM credentials c WHERE c.id = $1 AND c.domain_id = $2)
    OR
    EXISTS (SELECT 1 FROM credential_domains cd WHERE cd.credential_id = $1 AND cd.domain_id = $2)
) AS covers;
