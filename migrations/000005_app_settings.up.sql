-- Runtime-editable settings (key/value JSON), e.g. the retention policy set from
-- the WebUI Settings screen. Distinct from static boot config in relay.toml.
CREATE TABLE app_settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
