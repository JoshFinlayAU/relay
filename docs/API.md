# Relay API

A small REST API under `/v1`. JSON in, JSON out. Everything an operator can do in
the WebUI is available here. The sections below walk the common flow — **create an
API key → onboard a domain → publish DNS → verify → send / receive** — and a
**[full endpoint reference](#full-endpoint-reference)** at the end lists every route.

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
  "verified": true,
  "results": [
    {"purpose":"ownership","result":"pass","observed":"relay-verify=…","detail":""},
    {"purpose":"spf","result":"pass","observed":"v=spf1 …","detail":""}
  ]
}
```

Success is the top-level boolean **`active`** (also returned as **`verified`** —
they're identical). `result` is `pass` / `warn` / `fail` / `unknown` per record
(only a `fail` on a required record blocks activation; `warn` still passes). If
`active`/`verified` is `false`, fix the failing records and call verify again.

> Note: verification queries the domain's **authoritative** nameservers directly
> (not a cache). For a *delegated subdomain* it uses the delegated zone's
> nameservers; those must be reachable from the relay host.

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

## TLS certificates

The **server-hostname** cert is configured in `relay.toml` (`acme_enabled`, or
`tls_cert_file`/`tls_key_file`). After swapping renewed files, hot-reload with no
restart:

```bash
curl -s https://$HOST/v1/settings/tls -H "Authorization: Bearer $KEY"          # status
curl -sX POST https://$HOST/v1/settings/tls/reload -H "Authorization: Bearer $KEY"  # hot reload
```

**Per-hosted-domain** certs are served by SNI (falling back to the server cert):

```bash
# Upload / replace (PEM; cert_pem is the full chain, leaf first)
curl -sX PUT https://$HOST/v1/domains/<id>/tls-cert -H "Authorization: Bearer $KEY" \
  -d '{"cert_pem":"-----BEGIN CERTIFICATE-----\n…","key_pem":"-----BEGIN PRIVATE KEY-----\n…"}'
curl -s  https://$HOST/v1/domains/<id>/tls-cert -H "Authorization: Bearer $KEY"   # status
curl -sX DELETE https://$HOST/v1/domains/<id>/tls-cert -H "Authorization: Bearer $KEY"
```

The private key is sealed at rest and never returned.

## DMARC analyzer

Relay sets every domain's DMARC `rua` to `dmarc@<mail-host>` and ingests the
aggregate (XML) reports mailbox providers send back. Per-domain analysis:

```bash
curl -s "https://$HOST/v1/domains/<id>/dmarc?window=30d" -H "Authorization: Bearer $KEY"
```

```json
{
  "window": "30d",
  "summary": {"total":1420,"passed":1408,"dkim_pass":1400,"spf_pass":1290,"quarantined":8,"rejected":4},
  "top_sources": [{"source_ip":"203.0.113.10","total":1200,"passed":1200}],
  "reports":     [{"org_name":"google.com","date_end":"…","policy_p":"none","messages":420}]
}
```

`passed` = messages where aligned DKIM **or** SPF passed. Operator setup (one
time): publish `*._report._dmarc.<mail-host> TXT "v=DMARC1"` (shown in
**Settings → Server DNS**) so external reporters are authorised to send reports
to `dmarc@<mail-host>`.

## Auto-configure DNS (Cloudflare)

Instead of publishing records by hand, Relay can create them in a Cloudflare zone
using a scoped API token (needs **Zone:Read + DNS:Edit** — the "Edit zone DNS"
template). The token is used once and never stored.

```bash
curl -sX POST https://$HOST/v1/domains/<id>/dns/provision \
  -H "Authorization: Bearer $KEY" \
  -d '{"provider":"cloudflare","api_token":"<cf-token>"}'
# → {"results":[{"purpose":"spf","action":"created"}, …]}
```

## Send: SMTP credentials

Apps send via SMTP (587 STARTTLS / 465 implicit TLS) using per-app credentials.
The secret is returned **once**.

```bash
# Create (restrictions optional: allowed_from, max_messages_per_hour,
# max_recipients, max_message_bytes)
curl -sX POST https://$HOST/v1/domains/<id>/credentials \
  -H "Authorization: Bearer $KEY" \
  -d '{"name":"orders","restrictions":{"max_messages_per_hour":1000}}'
# → {"credential":{"username":"orders@example.com",…},"secret":"…"}

curl -s https://$HOST/v1/domains/<id>/credentials -H "Authorization: Bearer $KEY"   # list
curl -s https://$HOST/v1/credentials/<cid> -H "Authorization: Bearer $KEY"          # get
curl -sX PATCH https://$HOST/v1/credentials/<cid> -H "Authorization: Bearer $KEY" \
  -d '{"status":"suspended"}'                                                        # suspend/resume/revoke + restrictions
curl -sX DELETE https://$HOST/v1/credentials/<cid> -H "Authorization: Bearer $KEY"  # delete
curl -s "https://$HOST/v1/credentials/<cid>/stats?window=7d" -H "Authorization: Bearer $KEY"
```

## Receive: mailboxes & webhooks

To receive inbound mail for a `receiving` domain, create a mailbox with a webhook
URL. Relay POSTs parsed inbound mail to it (HMAC-signed with the returned secret).
Use `"local_part":"*"` for a catch-all.

```bash
# Create (secret optional; generated if omitted, returned once)
curl -sX POST https://$HOST/v1/domains/<id>/mailboxes \
  -H "Authorization: Bearer $KEY" \
  -d '{"local_part":"support","webhook_url":"https://app.example.com/inbound"}'
# → {"mailbox":{…},"secret":"whsec_…"}

curl -s https://$HOST/v1/domains/<id>/mailboxes -H "Authorization: Bearer $KEY"     # list
# Change the webhook URL / rotate the secret (secret optional):
curl -sX PATCH https://$HOST/v1/mailboxes/<mid> -H "Authorization: Bearer $KEY" \
  -d '{"webhook_url":"https://app.example.com/inbound-v2"}'
curl -sX DELETE https://$HOST/v1/mailboxes/<mid> -H "Authorization: Bearer $KEY"    # delete
```

The webhook body is JSON `{message_id, from, to, subject, headers, text, html,
attachments[], raw_size, spf_result, dkim_result}`, signed with
`X-Relay-Signature: sha256=HMAC(secret, "<X-Relay-Timestamp>.<body>")`. Delivery
log + manual re-fire:

```bash
curl -s https://$HOST/v1/domains/<id>/webhook-deliveries -H "Authorization: Bearer $KEY"
curl -sX POST https://$HOST/v1/webhook-deliveries/<wid>/redeliver -H "Authorization: Bearer $KEY"
```

## Messages

Search and inspect sent/received mail and its delivery history.

```bash
# List/search (filters: direction, status, from, subject, rcpt, after, before,
# domain_id, credential_id; paginated).
curl -s "https://$HOST/v1/messages?status=bounced&from=orders&after=2026-07-01T00:00:00Z" \
  -H "Authorization: Bearer $KEY"

# Detail: outbound delivery attempts (MX, SMTP code), bounces, and — for inbound —
# webhook_deliveries + spf_result/dkim_result.
curl -s https://$HOST/v1/messages/<mid> -H "Authorization: Bearer $KEY"
curl -s https://$HOST/v1/messages/<mid>/raw -H "Authorization: Bearer $KEY"   # raw headers (text)
```

## Suppressions

Per-domain suppressed recipients (hard bounces / complaints are auto-added).

```bash
curl -s  https://$HOST/v1/domains/<id>/suppressions -H "Authorization: Bearer $KEY"
curl -sX POST   https://$HOST/v1/domains/<id>/suppressions -H "Authorization: Bearer $KEY" -d '{"address":"a@x.com"}'
curl -sX DELETE https://$HOST/v1/domains/<id>/suppressions -H "Authorization: Bearer $KEY" -d '{"address":"a@x.com"}'
```

## Stats, events & test send

```bash
curl -s "https://$HOST/v1/domains/<id>/stats?window=24h" -H "Authorization: Bearer $KEY"
curl -s "https://$HOST/v1/domains/<id>/stats/timeseries?window=7d" -H "Authorization: Bearer $KEY"
curl -s https://$HOST/v1/stats/overview -H "Authorization: Bearer $KEY"   # dashboard rollup
curl -s "https://$HOST/v1/events?limit=100" -H "Authorization: Bearer $KEY"
curl -s https://$HOST/v1/server/info -H "Authorization: Bearer $KEY"      # hostname, listeners, cert, server DNS

# Fire a test message from a domain (self-onboarding smoke test):
curl -sX POST https://$HOST/v1/domains/<id>/test-send -H "Authorization: Bearer $KEY" \
  -d '{"to":"dest@example.net"}'   # → {"message_id":"…","trace_url":"/v1/messages/…"}
```

## Retention

Message-retention policy (keep by age or by count). See the WebUI **Settings →
Message retention**.

```bash
curl -s https://$HOST/v1/settings/retention -H "Authorization: Bearer $KEY"
curl -sX PUT https://$HOST/v1/settings/retention -H "Authorization: Bearer $KEY" \
  -d '{"enabled":true,"mode":"age","days":90}'          # or {"mode":"count","max_messages":100000}
```

## Conventions

- **Errors:** `{"error":{"code":"…","message":"…"}}` with the matching HTTP status
  (`400` bad input, `401` unauthenticated, `404` not found, `409` conflict).
- **Pagination:** list endpoints take `?limit=` (1–200, default 50) and `?cursor=`,
  and return `next_cursor` (empty when there are no more pages).
- **Timestamps:** RFC 3339 UTC.
- Secrets (API keys, credential secrets, webhook secrets, DKIM private keys) are
  never returned after creation.

## Full endpoint reference

Ops (no auth): `GET /healthz` · `GET /v1/ping`. `GET /metrics` is behind admin
auth on the main mux, or on a separate listener when `metrics_addr` is set.

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/auth/login` | Log in (username/password) → session token |
| POST | `/v1/auth/logout` | Invalidate the session token |
| GET | `/v1/auth/verify` | Validate the current token |
| GET/POST | `/v1/api-keys` | List / create API keys (secret shown once) |
| DELETE | `/v1/api-keys/{id}` | Revoke an API key |
| GET/POST | `/v1/admin/users` | List / create admin (WebUI) users |
| POST | `/v1/admin/users/{id}/password` | Change an admin user's password |
| DELETE | `/v1/admin/users/{id}` | Delete an admin user |
| GET/POST | `/v1/domains` | List / onboard domains (create returns DNS records) |
| GET | `/v1/domains/{id}` | Domain detail |
| PATCH | `/v1/domains/{id}` | receiving / sending_paused / forward_bounces / delivery_max_age_seconds |
| DELETE | `/v1/domains/{id}` | Delete a domain |
| GET | `/v1/domains/{id}/dns` | DNS records + live per-record check status |
| POST | `/v1/domains/{id}/dns/provision` | Auto-create records at Cloudflare |
| POST | `/v1/domains/{id}/verify` | Verify DNS → activate (`active`/`verified`) |
| GET | `/v1/domains/{id}/stats` · `/stats/timeseries` | Windowed stats / hourly series |
| GET | `/v1/domains/{id}/dmarc` | DMARC analyzer (summary, sources, reports) |
| POST | `/v1/domains/{id}/test-send` | Send a test message |
| GET/PUT/DELETE | `/v1/domains/{id}/tls-cert` | Per-domain TLS cert (SNI) |
| GET/POST | `/v1/domains/{id}/credentials` | List / create SMTP credentials |
| GET/PATCH/DELETE | `/v1/credentials/{id}` | Get / update (status, restrictions) / delete |
| GET | `/v1/credentials/{id}/stats` | Per-credential delivery stats |
| GET/POST | `/v1/domains/{id}/mailboxes` | List / create inbound mailboxes |
| PATCH/DELETE | `/v1/mailboxes/{id}` | Update webhook (URL / rotate secret) / delete |
| GET | `/v1/domains/{id}/webhook-deliveries` | Webhook delivery log |
| POST | `/v1/webhook-deliveries/{id}/redeliver` | Re-fire a webhook |
| GET/POST/DELETE | `/v1/domains/{id}/suppressions` | List / add / remove suppressions |
| GET | `/v1/messages` · `/v1/messages/{id}` · `/{id}/raw` | Search / detail / raw headers |
| GET | `/v1/stats/overview` | Dashboard rollup |
| GET | `/v1/events` | Audit trail |
| GET | `/v1/server/info` | Hostname, listeners, cert status, server DNS records |
| GET/PUT | `/v1/settings/retention` | Message-retention policy |
| GET | `/v1/settings/tls` | Server-cert source + expiry |
| POST | `/v1/settings/tls/reload` | Hot-reload certs (no restart) |
