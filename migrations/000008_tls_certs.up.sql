-- Operator-supplied (manual) TLS certificates for HOSTED DOMAINS, served by SNI.
-- The server-hostname cert is configured in relay.toml (acme_enabled /
-- tls_cert_file / tls_key_file), not here.
CREATE TABLE tls_certs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id  uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    cert_pem   text NOT NULL,              -- full chain (leaf first)
    key_enc    bytea NOT NULL,             -- sealed private-key PEM (AES-256-GCM)
    subjects   text[] NOT NULL DEFAULT '{}',
    not_before timestamptz,
    not_after  timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id)
);
