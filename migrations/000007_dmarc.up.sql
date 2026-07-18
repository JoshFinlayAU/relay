-- Ingested DMARC aggregate (RUA) reports, one row per report, matched to a
-- local domain by the report's policy_published.domain.
CREATE TABLE dmarc_reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id   uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    org_name    text NOT NULL,
    report_id   text NOT NULL,
    date_begin  timestamptz NOT NULL,
    date_end    timestamptz NOT NULL,
    policy_p    text,                       -- published policy (none|quarantine|reject)
    policy_pct  integer,
    raw_ref     text,                       -- stored raw report XML
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id, org_name, report_id)
);
CREATE INDEX dmarc_reports_domain ON dmarc_reports(domain_id, date_end DESC);

-- Per-source-IP evaluated rows within a report.
CREATE TABLE dmarc_report_rows (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id    uuid NOT NULL REFERENCES dmarc_reports(id) ON DELETE CASCADE,
    domain_id    uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    source_ip    text NOT NULL,
    message_count integer NOT NULL DEFAULT 0,
    disposition  text NOT NULL DEFAULT 'none',   -- none|quarantine|reject
    dkim_result  text NOT NULL DEFAULT 'none',   -- aligned DKIM (pass|fail|none)
    spf_result   text NOT NULL DEFAULT 'none',   -- aligned SPF
    header_from  text,
    row_end      timestamptz NOT NULL             -- report date_end, for windowing
);
CREATE INDEX dmarc_rows_domain ON dmarc_report_rows(domain_id, row_end DESC);
