-- Per-domain override for how long a queued message may keep deferring before it
-- is failed/bounced. NULL = use the global delivery_max_age from config.
ALTER TABLE domains ADD COLUMN delivery_max_age_seconds integer;
