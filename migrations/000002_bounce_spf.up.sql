-- Allow a recommended SPF record on the bounce subdomain (defence-in-depth so
-- the VERP envelope also yields spf=pass; DMARC already passes via DKIM).
BEGIN;

ALTER TABLE dns_records DROP CONSTRAINT IF EXISTS dns_records_purpose_check;
ALTER TABLE dns_records ADD CONSTRAINT dns_records_purpose_check
    CHECK (purpose IN ('ownership','dkim','spf','dmarc','bounce_mx','bounce_spf','inbound_mx'));

COMMIT;
