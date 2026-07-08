BEGIN;

DROP TABLE IF EXISTS stat_rollups;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS suppressions;
DROP TABLE IF EXISTS bounce_events;
DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS delivery_jobs;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS mailboxes;
DROP TABLE IF EXISTS credential_domains;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS dns_records;
DROP TABLE IF EXISTS dkim_keys;
DROP TABLE IF EXISTS domains;

DROP FUNCTION IF EXISTS set_updated_at();

COMMIT;
