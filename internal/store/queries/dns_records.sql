-- name: UpsertDNSRecord :exec
INSERT INTO dns_records (domain_id, purpose, type, name, expected_value)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (domain_id, purpose)
DO UPDATE SET type = EXCLUDED.type, name = EXCLUDED.name, expected_value = EXCLUDED.expected_value;

-- name: ListDNSRecords :many
SELECT * FROM dns_records WHERE domain_id = $1 ORDER BY purpose;

-- name: UpdateDNSRecordResult :exec
UPDATE dns_records
SET observed_value = $3, last_result = $4, detail = $5, last_checked = now()
WHERE domain_id = $1 AND purpose = $2;

-- name: DeleteDNSRecords :exec
DELETE FROM dns_records WHERE domain_id = $1;
