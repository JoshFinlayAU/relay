# Relay Operations Runbook

Operational procedures for `relayd` on the production VM (`mail.as135559.net.au`,
`160.30.37.130` / `2001:df4:2040:5::2`).

## 1. Pre-flight (before first send)

- [ ] **PTR / reverse DNS** for both send IPs resolves to `mail.as135559.net.au`,
      and forward `A`/`AAAA` match back (FCrDNS). Verify:
      `dig -x 160.30.37.130` and `dig -x 2001:df4:2040:5::2`.
- [ ] **Port 25 egress** allowed by the DC/upstream (test: `nc -zv gmail-smtp-in.l.google.com 25`).
- [ ] **SPF include target** `spf.mail.as135559.net.au` published with the send IPs.
- [ ] **IP not on major blocklists** - check Spamhaus, Barracuda, SORBS, and
      Microsoft SNDS / Google Postmaster Tools enrolment.
- [ ] `RELAY_SECRET_KEY` set (32-byte base64) and backed up out-of-band - losing
      it makes stored DKIM keys and webhook secrets unrecoverable.

## 2. IP / domain warm-up

New IPs and domains have no reputation; ramp gradually:

| Days | Daily volume ceiling |
|---|---|
| 1–2 | ~50 |
| 3–4 | ~200 |
| 5–7 | ~1,000 |
| 2nd week | ~5,000 |
| 3rd week+ | double every 2–3 days if complaint rate < 0.1% |

- Keep the DMARC policy at `p=none` during warm-up; move to `p=quarantine`
  once Google Postmaster shows a good domain reputation, then `p=reject`.
- Watch `relay_deferrals_by_domain_total` and Postmaster Tools daily.
- Send to engaged recipients first (real transactional mail), not test blasts.

## 3. Monitoring & alerts (NOCgenie / Prometheus)

Scrape `:8080/metrics` (or the metrics addr). Suggested alert rules:

- `relay_queue_depth > 500 for 10m` - delivery backing up.
- `rate(relay_deferrals_by_domain_total[15m]) > 0.2 * rate(relay_delivered_total[15m])` - a destination is throttling us.
- `increase(relay_failed_total[1h]) > 50` - spike in permanent failures (bad list / blocklisting).
- `increase(relay_webhooks_dead_letter_total[1h]) > 0` - a customer webhook is down.
- Certificate expiry: alert if the LE cert has < 14 days left (certmagic auto-renews at 30 days).
- `up == 0` - relayd down.

## 4. Backup & restore

- **Postgres**: nightly `pg_dump` into PBS. Contains all state (domains, DKIM
  keys [encrypted], credentials, messages metadata, suppressions, webhooks).
  Restore: `pg_restore` into a fresh PG16, then start relayd (auto-migrates).
- **Message bodies**: `storage/` (content-addressed) is in the VM/PBS backup.
  Not required for service to start; only for historical message retrieval.
- **`RELAY_SECRET_KEY`**: stored in the secrets manager, NOT in the DB backup.
  Restore is useless without it (DKIM keys stay encrypted).
- **Drill quarterly**: restore the latest dump to a scratch VM, start relayd,
  confirm `/healthz` green and a domain verifies.

## 5. DKIM key rotation

Selectors are year-stamped (`rly2026a`). To rotate without downtime:

1. Generate a new key/selector (e.g. `rly2026b`) for the domain (new API/UI action
   - or insert a second `dkim_keys` row and publish its DNS record).
2. **Publish** the new selector's TXT record; leave the old one in place.
3. Wait for DNS propagation + in-flight mail signed with the old key to clear
   (≥ 1 day).
4. **Cut over**: mark the new key active (`active=true`), old key `active=false`.
   New mail signs with the new selector.
5. After a further ~7 days (no more mail references the old selector), remove the
   old DNS record and delete the old key row.

## 6. Certificate management

- certmagic obtains/renews the Let's Encrypt cert automatically (HTTP-01 on :80).
- Renewal happens in the background ~30 days before expiry; logged as
  `certificate obtained successfully`.
- If issuance fails: confirm :80 is reachable from the internet and DNS for the
  hostname resolves to this box. Use `RELAY_ACME_STAGING=true` to debug without
  burning production rate limits.

## 7. Incident: sudden deferrals from one provider

1. Check `relay_deferrals_by_domain_total` for the destination.
2. Inspect recent `delivery_attempts` for that domain (`smtp_code`/`smtp_response`).
3. If reputation-related (4xx greylisting/throttling), the retry schedule handles
   it; reduce volume to that provider and check Postmaster Tools.
4. If blocklisted, pause sending for affected domains (Domains → pause), request
   delisting, and investigate the trigger (spike, bad list, compromised credential).

## 8. Deploy

Single static binary + systemd unit. `make all` builds SPA + binary. Roll by
replacing the binary and `systemctl restart relayd` - graceful shutdown drains
listeners then in-flight deliveries.
