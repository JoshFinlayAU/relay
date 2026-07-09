# Relay API

A small REST API under `/v1`. JSON in, JSON out. Everything an operator can do in
the WebUI is available here — this doc focuses on the common flow: **create an API
key → onboard a domain → publish DNS → verify → send / receive.**

Base URL: `https://<your-host>` (e.g. `https://mail.as135559.net.au`).

## Authentication

All `/v1` endpoints (except `/healthz` and `/v1/auth/login`) require a bearer token:

```
Authorization: Bearer <token>
```

A token can be a static config token (`admin_tokens` in `relay.toml`) or a
**managed API key**. Create API keys in the WebUI (**API keys** screen) or via the
API. The secret is shown **once** — store it securely; only its SHA-256 is kept.

```bash
# Create a key (using an existing token / your WebUI login token)
curl -sX POST https://$HOST/v1/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"provisioning-bot"}'
# → {"api_key":{"id":"…","name":"provisioning-bot",…},"token":"relay_XXXXXXXX"}

curl -s https://$HOST/v1/api-keys -H "Authorization: Bearer $TOKEN"        # list
curl -sX DELETE https://$HOST/v1/api-keys/<id> -H "Authorization: Bearer $TOKEN"  # revoke
```

Use the returned `token` as the bearer for everything below (`export KEY=relay_…`).

## Onboard a domain

```bash
curl -sX POST https://$HOST/v1/domains \
  -H "Authorization: Bearer $KEY" \
  -d '{"name":"example.com","receiving":false}'
```

Returns the domain plus **every DNS record to publish** (`dns[]`), each with a
`name`, `type`, `value`, and a ready-to-paste `zone_line`:

```json
{
  "domain": { "id": "…", "name": "example.com", "status": "pending", … },
  "dns": [
    {"purpose":"ownership","type":"TXT","name":"_relay-verify.example.com","value":"relay-verify=…","required":true},
    {"purpose":"dkim","type":"TXT","name":"rly2026a._domainkey.example.com","value":"v=DKIM1; k=rsa; p=…","required":true},
    {"purpose":"spf","type":"TXT","name":"example.com","value":"v=spf1 include:spf.<your-host> ~all","required":true},
    {"purpose":"dmarc","type":"TXT","name":"_dmarc.example.com","value":"v=DMARC1; p=none; rua=…","required":false},
    {"purpose":"bounce_mx","type":"MX","name":"bounce.example.com","value":"10 <your-host>.","required":true}
  ]
}
```

Set `"receiving":true` to also get the inbound `MX` record for the apex.

Re-fetch the records (with live per-record check status) any time:

```bash
curl -s https://$HOST/v1/domains/<id>/dns -H "Authorization: Bearer $KEY"
```

## Verify (activate)

After publishing the records, ask Relay to check them against authoritative DNS.
The domain moves to `active` once ownership + DKIM + SPF + bounce MX pass.

```bash
curl -sX POST https://$HOST/v1/domains/<id>/verify -H "Authorization: Bearer $KEY"
```

```json
{
  "domain": { "id": "…", "status": "active", … },
  "active": true,
  "results": [
    {"purpose":"ownership","result":"pass","observed":"relay-verify=…","detail":""},
    {"purpose":"spf","result":"pass","observed":"v=spf1 …","detail":""}
  ]
}
```

`result` is `pass` / `warn` / `fail` / `unknown` per record. If `active` is
`false`, fix the `fail`/`unknown` records and call verify again.

The **A/AAAA/SPF records for the mail host itself** (and the PTR requirement) are
shown in the WebUI under **Settings → Server DNS** / `GET /v1/server/info`.

### Domain settings

`PATCH /v1/domains/{id}` toggles per-domain settings:

```bash
curl -sX PATCH https://$HOST/v1/domains/<id> \
  -H "Authorization: Bearer $KEY" \
  -d '{"receiving":true,"sending_paused":false,"forward_bounces":false,
       "delivery_max_age_seconds":86400}'
```

- `delivery_max_age_seconds` — how long a deferred message keeps retrying before
  it is failed/bounced, for this domain (60–2592000s). Send `<=0` to clear the
  override and fall back to the server default (`delivery_max_age` in config).
  `GET /v1/domains/{id}` reports it (`null` = using the default).

## Send: create an SMTP credential

Apps send via SMTP (587 STARTTLS / 465 implicit TLS) using per-app credentials.
The secret is returned **once**.

```bash
curl -sX POST https://$HOST/v1/domains/<id>/credentials \
  -H "Authorization: Bearer $KEY" \
  -d '{"name":"orders","restrictions":{"max_messages_per_hour":1000}}'
# → {"credential":{"username":"orders@example.com",…},"secret":"…"}
```

## Receive: mailboxes & webhook

To receive inbound mail for a `receiving` domain, create a mailbox with a webhook
URL. Relay POSTs parsed inbound mail to it (HMAC-signed with the returned secret).
Use `"local_part":"*"` for a catch-all. Settable here and in the WebUI
(**Mailboxes** on the domain screen).

```bash
curl -sX POST https://$HOST/v1/domains/<id>/mailboxes \
  -H "Authorization: Bearer $KEY" \
  -d '{"local_part":"support","webhook_url":"https://app.example.com/inbound"}'
# → {"mailbox":{…},"secret":"whsec_…"}   (verify X-Relay-Signature: sha256=HMAC(secret,"<ts>.<body>"))
```

## Conventions

- **Errors:** `{"error":{"code":"…","message":"…"}}` with the matching HTTP status
  (`400` bad input, `401` unauthenticated, `404` not found, `409` conflict).
- **Pagination:** list endpoints take `?limit=` (1–200, default 50) and `?cursor=`,
  and return `next_cursor` (empty when there are no more pages).
- **Timestamps:** RFC 3339 UTC.
- Secrets (API keys, credential secrets, webhook secrets, DKIM private keys) are
  never returned after creation.
