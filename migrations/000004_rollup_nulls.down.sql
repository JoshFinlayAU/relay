BEGIN;
DROP INDEX IF EXISTS stat_rollups_uniq;
ALTER TABLE stat_rollups ADD CONSTRAINT stat_rollups_bucket_domain_id_credential_id_key
    UNIQUE (bucket, domain_id, credential_id);
COMMIT;
