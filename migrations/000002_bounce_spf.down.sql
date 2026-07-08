BEGIN;

DELETE FROM dns_records WHERE purpose = 'bounce_spf';
ALTER TABLE dns_records DROP CONSTRAINT IF EXISTS dns_records_purpose_check;
ALTER TABLE dns_records ADD CONSTRAINT dns_records_purpose_check
    CHECK (purpose IN ('ownership','dkim','spf','dmarc','bounce_mx','inbound_mx'));

COMMIT;
