-- Relay initial schema. All core tables per CLAUDE.md (some unused until later phases).
-- gen_random_uuid() is built into PostgreSQL 13+.

BEGIN;

-- updated_at maintenance ---------------------------------------------------
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- domains ------------------------------------------------------------------
CREATE TABLE domains (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL UNIQUE,
    status           text NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending','active','degraded','suspended')),
    receiving        boolean NOT NULL DEFAULT false,
    verify_token     text NOT NULL,
    bounce_subdomain text NOT NULL,
    forward_bounces  boolean NOT NULL DEFAULT false,
    sending_paused   boolean NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER domains_updated BEFORE UPDATE ON domains
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- dkim_keys ----------------------------------------------------------------
CREATE TABLE dkim_keys (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id       uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    selector        text NOT NULL,
    algorithm       text NOT NULL DEFAULT 'rsa' CHECK (algorithm IN ('rsa','ed25519')),
    private_key_enc bytea NOT NULL,          -- AES-256-GCM ciphertext, never exposed via API
    public_key      text NOT NULL,           -- base64 DER SPKI, for the DNS p= value
    active          boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id, selector)
);

-- dns_records --------------------------------------------------------------
CREATE TABLE dns_records (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id      uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    purpose        text NOT NULL
                     CHECK (purpose IN ('ownership','dkim','spf','dmarc','bounce_mx','inbound_mx')),
    type           text NOT NULL,            -- TXT | MX
    name           text NOT NULL,            -- FQDN of the record
    expected_value text NOT NULL,
    observed_value text,
    last_checked   timestamptz,
    last_result    text NOT NULL DEFAULT 'unknown'
                     CHECK (last_result IN ('unknown','pass','fail','warn')),
    detail         text,
    UNIQUE (domain_id, purpose)
);
CREATE INDEX dns_records_domain ON dns_records(domain_id);

-- credentials --------------------------------------------------------------
CREATE TABLE credentials (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id         uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    username          text NOT NULL UNIQUE,  -- <app>@<domain> or opaque key id
    secret_hash       text NOT NULL,         -- argon2id
    restrictions      jsonb NOT NULL DEFAULT '{}'::jsonb,
    status            text NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active','suspended','revoked')),
    last_used         timestamptz,
    failed_auth_count integer NOT NULL DEFAULT 0,
    locked_until      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credentials_domain ON credentials(domain_id);
CREATE TRIGGER credentials_updated BEFORE UPDATE ON credentials
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- credential_domains (additional domain grants) ----------------------------
CREATE TABLE credential_domains (
    credential_id uuid NOT NULL REFERENCES credentials(id) ON DELETE CASCADE,
    domain_id     uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    PRIMARY KEY (credential_id, domain_id)
);

-- mailboxes ----------------------------------------------------------------
CREATE TABLE mailboxes (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id          uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    local_part         text NOT NULL,        -- '*' = catch-all
    webhook_url        text NOT NULL,
    webhook_secret_enc bytea NOT NULL,        -- encrypted at rest
    status             text NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active','suspended')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id, local_part)
);
CREATE TRIGGER mailboxes_updated BEFORE UPDATE ON mailboxes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- messages -----------------------------------------------------------------
CREATE TABLE messages (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    direction     text NOT NULL CHECK (direction IN ('outbound','inbound')),
    credential_id uuid REFERENCES credentials(id) ON DELETE SET NULL,
    domain_id     uuid REFERENCES domains(id) ON DELETE SET NULL,
    mail_from     text,                       -- envelope MAIL FROM (VERP for outbound)
    header_from   text,
    rcpt_to       text[] NOT NULL DEFAULT '{}',
    subject       text,
    size_bytes    bigint NOT NULL DEFAULT 0,
    dkim_selector text,
    body_ref      text,                       -- storage/msgs/ab/cd/<sha256>
    verp_token    text,                       -- for async bounce matching
    status        text NOT NULL DEFAULT 'queued',
    spf_result    text,                       -- inbound
    dkim_result   text,                       -- inbound
    queued_at     timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_domain      ON messages(domain_id);
CREATE INDEX messages_credential  ON messages(credential_id);
CREATE INDEX messages_status      ON messages(status);
CREATE INDEX messages_direction   ON messages(direction, created_at DESC);
CREATE INDEX messages_verp        ON messages(verp_token) WHERE verp_token IS NOT NULL;
CREATE TRIGGER messages_updated BEFORE UPDATE ON messages
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- delivery_jobs (the Postgres queue; claimed via SKIP LOCKED) ---------------
CREATE TABLE delivery_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id      uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    rcpt            text NOT NULL,
    status          text NOT NULL DEFAULT 'queued'
                      CHECK (status IN ('queued','in_progress','delivered','deferred','failed')),
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at       timestamptz,
    locked_by       text,
    last_code       integer,
    last_response   text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (message_id, rcpt)
);
CREATE INDEX delivery_jobs_claim ON delivery_jobs(next_attempt_at)
    WHERE status IN ('queued','deferred');
CREATE TRIGGER delivery_jobs_updated BEFORE UPDATE ON delivery_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- delivery_attempts (immutable history) ------------------------------------
CREATE TABLE delivery_attempts (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id    uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    rcpt          text NOT NULL,
    mx_host       text,
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    result        text NOT NULL CHECK (result IN ('delivered','deferred','failed')),
    smtp_code     integer,
    smtp_response text,
    tls_version   text,
    tls_verified  boolean
);
CREATE INDEX delivery_attempts_message ON delivery_attempts(message_id);

-- bounce_events ------------------------------------------------------------
CREATE TABLE bounce_events (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
    rcpt       text,
    type       text NOT NULL CHECK (type IN ('hard','soft','complaint')),
    dsn_code   text,
    raw_ref    text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bounce_events_message ON bounce_events(message_id);

-- suppressions -------------------------------------------------------------
CREATE TABLE suppressions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id  uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    address    text NOT NULL,
    reason     text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id, address)
);

-- webhook_deliveries -------------------------------------------------------
CREATE TABLE webhook_deliveries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mailbox_id       uuid NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    message_id       uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    attempt_no       integer NOT NULL DEFAULT 0,
    status_code      integer,
    result           text NOT NULL DEFAULT 'pending'
                       CHECK (result IN ('pending','success','failed','dead_letter')),
    next_attempt_at  timestamptz,
    response_snippet text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_deliveries_mailbox ON webhook_deliveries(mailbox_id);
CREATE INDEX webhook_deliveries_pending ON webhook_deliveries(next_attempt_at)
    WHERE result = 'pending';
CREATE TRIGGER webhook_deliveries_updated BEFORE UPDATE ON webhook_deliveries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- events (audit trail) -----------------------------------------------------
CREATE TABLE events (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id     uuid REFERENCES domains(id) ON DELETE SET NULL,
    credential_id uuid REFERENCES credentials(id) ON DELETE SET NULL,
    type          text NOT NULL,
    detail        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_created ON events(created_at DESC);
CREATE INDEX events_domain  ON events(domain_id);

-- api_tokens (Settings UI; static config tokens still supported) ------------
CREATE TABLE api_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    token_hash text NOT NULL UNIQUE,          -- sha256 of the bearer token
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used  timestamptz,
    revoked_at timestamptz
);

-- stat_rollups (hourly aggregates, populated in Phase 8) --------------------
CREATE TABLE stat_rollups (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket        timestamptz NOT NULL,       -- hour bucket
    domain_id     uuid REFERENCES domains(id) ON DELETE CASCADE,
    credential_id uuid REFERENCES credentials(id) ON DELETE CASCADE,
    submitted     bigint NOT NULL DEFAULT 0,
    delivered     bigint NOT NULL DEFAULT 0,
    deferred      bigint NOT NULL DEFAULT 0,
    bounced_hard  bigint NOT NULL DEFAULT 0,
    bounced_soft  bigint NOT NULL DEFAULT 0,
    complaints    bigint NOT NULL DEFAULT 0,
    UNIQUE (bucket, domain_id, credential_id)
);
CREATE INDEX stat_rollups_bucket ON stat_rollups(bucket DESC);

COMMIT;
