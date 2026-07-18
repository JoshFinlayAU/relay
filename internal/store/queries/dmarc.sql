-- name: UpsertDMARCReport :one
-- Idempotent per (domain, org, report_id). Returns (id, inserted?) — xmax=0
-- means a fresh insert so the caller only adds rows for new reports.
INSERT INTO dmarc_reports (domain_id, org_name, report_id, date_begin, date_end, policy_p, policy_pct, raw_ref)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (domain_id, org_name, report_id) DO UPDATE SET org_name = EXCLUDED.org_name
RETURNING id, (xmax = 0) AS inserted;

-- name: InsertDMARCRow :exec
INSERT INTO dmarc_report_rows
  (report_id, domain_id, source_ip, message_count, disposition, dkim_result, spf_result, header_from, row_end)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: DMARCSummary :one
-- Aggregate totals for a domain since a cutoff. DMARC "pass" = aligned DKIM OR
-- aligned SPF passed.
SELECT
  COALESCE(SUM(message_count), 0)::bigint AS total,
  COALESCE(SUM(message_count) FILTER (WHERE dkim_result = 'pass' OR spf_result = 'pass'), 0)::bigint AS passed,
  COALESCE(SUM(message_count) FILTER (WHERE dkim_result = 'pass'), 0)::bigint AS dkim_pass,
  COALESCE(SUM(message_count) FILTER (WHERE spf_result = 'pass'), 0)::bigint AS spf_pass,
  COALESCE(SUM(message_count) FILTER (WHERE disposition = 'quarantine'), 0)::bigint AS quarantined,
  COALESCE(SUM(message_count) FILTER (WHERE disposition = 'reject'), 0)::bigint AS rejected
FROM dmarc_report_rows
WHERE domain_id = $1 AND row_end >= $2;

-- name: DMARCTopSources :many
SELECT source_ip,
  COALESCE(SUM(message_count), 0)::bigint AS total,
  COALESCE(SUM(message_count) FILTER (WHERE dkim_result = 'pass' OR spf_result = 'pass'), 0)::bigint AS passed
FROM dmarc_report_rows
WHERE domain_id = $1 AND row_end >= $2
GROUP BY source_ip
ORDER BY total DESC
LIMIT $3;

-- name: ListDMARCReports :many
SELECT id, org_name, report_id, date_begin, date_end, policy_p, policy_pct, received_at,
  (SELECT COALESCE(SUM(message_count),0)::bigint FROM dmarc_report_rows r WHERE r.report_id = dmarc_reports.id) AS messages
FROM dmarc_reports
WHERE dmarc_reports.domain_id = $1
ORDER BY date_end DESC
LIMIT $2 OFFSET $3;
