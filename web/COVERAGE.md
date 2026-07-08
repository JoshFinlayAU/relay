# WebUI Coverage Matrix

Every `/v1` API endpoint must map to a UI surface and a Playwright spec. An
endpoint with no screen or no spec is an unchecked `TASKS.md` item.

Legend: ✅ done · 🚧 in progress · ⬜ not started

| Endpoint | Method | Screen / Component | Playwright spec | Status |
|---|---|---|---|---|
| `/healthz` | GET | Dashboard (backend status) | `e2e/smoke.spec.ts` | ✅ |
| `/v1/ping` | GET | (internal) | - | ✅ |
| `/v1/auth/login` | POST | Login (username/password) | `e2e/domains.spec.ts`, `e2e/users.spec.ts` | ✅ |
| `/v1/auth/logout` | POST | Layout (Sign out) | `e2e/users.spec.ts` | ✅ |
| `/v1/auth/verify` | GET | Login (token validation) | `e2e/smoke.spec.ts` | ✅ |
| `/v1/admin/users` | GET | Admin users (list) | `e2e/users.spec.ts` | ✅ |
| `/v1/admin/users` | POST | Admin users (add) | `e2e/users.spec.ts` | ✅ |
| `/v1/admin/users/{id}/password` | POST | Admin users (change password) | `e2e/users.spec.ts` | ✅ |
| `/v1/admin/users/{id}` | DELETE | Admin users (delete) | `e2e/users.spec.ts` | ✅ |
| `/v1/domains` | POST | Domains → Add-domain wizard | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains` | GET | Domains (list) | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}` | GET | DomainDetail | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}` | PATCH | DomainDetail (receiving/pause/forward toggles) | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}` | DELETE | DomainDetail (delete, confirmed) | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}/dns` | GET | DomainDetail → `DnsPanel` (traffic-light summary, collapsible records, SPF merge, operator note) | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}/verify` | POST | `DnsPanel` ("Verify now") | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}/dns/provision` | POST | `DnsPanel` → `CloudflarePanel` (auto-configure) | `e2e/domains.spec.ts` | ✅ |
| `/v1/domains/{id}/credentials` | POST | DomainDetail → Credentials (create + one-time secret) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/domains/{id}/credentials` | GET | DomainDetail → Credentials (list) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/credentials/{id}` | GET | (fetched into Credentials list) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/credentials/{id}` | PATCH | Credentials (suspend/resume/restrictions) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/credentials/{id}` | DELETE | Credentials (delete) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/credentials/{id}/stats` | GET | Credentials → `CredentialStats` (per-credential outcome bar + tiles, windowed) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/messages` | GET | Messages (list + status/direction filters) | `e2e/messages.spec.ts` | ✅ |
| `/v1/messages/{id}` | GET | MessageDetail (delivery timeline) | `e2e/messages.spec.ts` | ✅ |
| `/v1/messages/{id}/raw` | GET | MessageDetail → `RawHeaders` (view raw headers) | `e2e/system.spec.ts` | ✅ |
| `/v1/stats/overview` | GET | Dashboard (queue depth, status counts, events) | `e2e/messages.spec.ts` | ✅ |
| `/v1/domains/{id}/suppressions` | GET | DomainDetail → Suppressions | `e2e/suppressions.spec.ts` | ✅ |
| `/v1/domains/{id}/suppressions` | POST | DomainDetail → Suppressions (add) | `e2e/suppressions.spec.ts` | ✅ |
| `/v1/domains/{id}/suppressions` | DELETE | DomainDetail → Suppressions (remove/override) | `e2e/suppressions.spec.ts` | ✅ |
| `/v1/messages/{id}` (bounces) | GET | MessageDetail (bounce/complaint detail) | `e2e/messages.spec.ts` | ✅ |
| `/v1/domains/{id}/mailboxes` | POST | DomainDetail → Mailboxes (create + one-time secret) | `e2e/mailboxes.spec.ts` | ✅ |
| `/v1/domains/{id}/mailboxes` | GET | DomainDetail → Mailboxes (list) | `e2e/mailboxes.spec.ts` | ✅ |
| `/v1/mailboxes/{id}` | DELETE | Mailboxes (delete) | `e2e/mailboxes.spec.ts` | ✅ |
| `/v1/domains/{id}/webhook-deliveries` | GET | Mailboxes (delivery log) | `e2e/mailboxes.spec.ts` | ✅ |
| `/v1/webhook-deliveries/{id}/redeliver` | POST | Mailboxes (re-deliver dead-letter) | (manual) | ✅ |

## Pending (added as phases land)
- Phase 2: `POST/GET/DELETE /v1/domains/{id}/credentials`, revoke/suspend,
  `GET /v1/credentials/{id}/stats` → Credentials screens.
- Phase 4: `GET /v1/messages`, `GET /v1/messages/{id}` → Messages screens; Dashboard stats.
- Phase 5: suppressions endpoints → Suppressions screen.
- Phase 6: `POST/GET/DELETE /v1/domains/{id}/mailboxes`, webhook delivery log → Mailboxes screens.
- Phase 8/9: `GET /v1/stats/overview`, message search, test-send, events, settings, API tokens.

## Phase 8/9 endpoints - now with UI surfaces

| Endpoint | Method | Screen / Component | Playwright spec | Status |
|---|---|---|---|---|
| `/v1/domains/{id}/stats` | GET | DomainDetail → DomainStats (tiles) | `e2e/system.spec.ts` | ✅ |
| `/v1/domains/{id}/stats/timeseries` | GET | DomainStats (Recharts area chart) | `e2e/system.spec.ts` | ✅ |
| `/v1/credentials/{id}/stats` | GET | Credentials → `CredentialStats` (per-credential chart) | `e2e/credentials.spec.ts` | ✅ |
| `/v1/domains/{id}/test-send` | POST | DomainStats (Test send) | `e2e/system.spec.ts` | ✅ |
| `/v1/messages` (`after`/`before`/`rcpt`) | GET | Messages (recipient search) | `e2e/messages.spec.ts` | ✅ |
| `/v1/events` | GET | Events screen | `e2e/system.spec.ts` | ✅ |
| `/v1/server/info` | GET | Settings screen | `e2e/system.spec.ts` | ✅ |
| `/v1/settings/retention` | GET | Settings → Message retention | `e2e/system.spec.ts` | ✅ |
| `/v1/settings/retention` | PUT | Settings → Message retention (save) | `e2e/system.spec.ts` | ✅ |

**Coverage: every `/v1` endpoint now has a UI surface + spec.** Remaining polish
(charts styling, code-splitting, per-credential charts, date-range picker) is Phase 11.
