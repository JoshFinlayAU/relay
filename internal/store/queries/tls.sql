-- name: ListTLSCerts :many
SELECT id, domain_id, cert_pem, key_enc, subjects, not_before, not_after FROM tls_certs;

-- name: GetDomainTLSCert :one
SELECT * FROM tls_certs WHERE domain_id = $1;

-- name: DeleteDomainTLSCert :execrows
DELETE FROM tls_certs WHERE domain_id = $1;

-- name: UpsertDomainTLSCert :one
INSERT INTO tls_certs (domain_id, cert_pem, key_enc, subjects, not_before, not_after)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (domain_id) DO UPDATE SET
  cert_pem = EXCLUDED.cert_pem, key_enc = EXCLUDED.key_enc, subjects = EXCLUDED.subjects,
  not_before = EXCLUDED.not_before, not_after = EXCLUDED.not_after, updated_at = now()
RETURNING *;
