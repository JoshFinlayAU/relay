-- Per-domain rollups use a NULL credential_id. Default UNIQUE treats NULLs as
-- distinct, breaking ON CONFLICT; PG16 NULLS NOT DISTINCT fixes it.
BEGIN;

ALTER TABLE stat_rollups DROP CONSTRAINT IF EXISTS stat_rollups_bucket_domain_id_credential_id_key;
DROP INDEX IF EXISTS stat_rollups_uniq;
CREATE UNIQUE INDEX stat_rollups_uniq
    ON stat_rollups (bucket, domain_id, credential_id) NULLS NOT DISTINCT;

COMMIT;
