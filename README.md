# Relay

A self-hosted transactional mail platform - a single Go binary that hosts
multiple sending domains, issues per-app SMTP credentials, DKIM-signs and delivers
mail directly to destination MXes, and optionally receives inbound mail per-mailbox
and forwards it to a webhook. A React WebUI (embedded via `go:embed`) covers every
API capability.

Not a general-purpose MTA: no IMAP/POP3, no local mailboxes, no relaying for
arbitrary users.

## Install (one command)

```bash
git clone <repo> && cd relay
./install.sh
```

`install.sh` installs the prerequisites (Go, Node.js, PostgreSQL), provisions the
`relay` role + database, generates a `0600` **`relay.toml`** with a fresh secret
key / admin password / API token, builds the SPA + binary, and installs and
starts a **systemd service** (`relayd.service`) that runs the binary. It's
idempotent - safe to re-run - and prints the generated login details.

Secrets live in `relay.toml` and are read via `RELAY_CONFIG`, so the database
password, secret key, admin password, and tokens **never appear in `ps` or the
process environment**.

```bash
sudo systemctl status relayd     # it's already running after install
journalctl -u relayd -f          # logs
```

Options: `--hostname <fqdn>` sets the mail host name (also derives SPF/DMARC/IPs);
`--tls` provisions Let's Encrypt on `:443` + the mail ports (`--acme-email` for
the contact); `--public-ipv4/--public-ipv6` or `--detect-public-ip` set the
sending IPs (see below); `--no-service` skips the systemd unit and just builds +
configures (run it yourself with `RELAY_CONFIG=./relay.toml ./relayd`);
`--with-test-db` also creates the `relay_test` database for `make test`.
Debian/Ubuntu for the package steps; on other distros it skips the installs and
just builds if Go/Node/Postgres are already present.

Health: `curl localhost:8080/healthz` · Metrics: authenticated `curl -H "Authorization: Bearer <token>" localhost:8080/metrics`

### Server identity & public IP

Relay derives what a sending host needs from the `hostname` in `relay.toml`: the
SPF include target (`spf.<hostname>`), the DMARC address
(`dmarc@<registrable-domain>`), and - from the host's own interfaces - the
sending IPv4/IPv6. **Settings → Server DNS** lists the exact `A`/`AAAA`/SPF
records to publish for the mail host, plus the PTR reminder. Set a real hostname
up front for production:

```bash
sudo ./install.sh --hostname mail.example.com --tls --acme-email ops@example.com
```

**Behind NAT / port-forwarding**, the host only sees its private IP, so
auto-detection can't find the public one - give it explicitly:

```bash
./install.sh --hostname mail.example.com --public-ipv4 203.0.113.10
# or let it ask the internet (opt-in; needs egress):
./install.sh --hostname mail.example.com --detect-public-ip
```

The value lands in `relay.toml` (`sending_ipv4` / `sending_ipv6`) and can be
edited any time (`sudo systemctl restart relayd` to apply). If nothing is set and
no public IP is found, **Settings → Server DNS** shows a warning rather than
emitting wrong records. Note IPv6 auto/echo detection may return a temporary
(privacy/SLAAC) address - pin `sending_ipv6` to the address your PTR points at.

### Manual (dev)

```bash
make all                              # build SPA + binary
cp relay.toml.example relay.toml       # fill in secret_key, database_url, admin_* ; chmod 600
RELAY_CONFIG=./relay.toml ./relayd     # auto-applies migrations
```

Config precedence is: built-in defaults → `relay.toml` (path in `RELAY_CONFIG`) →
`RELAY_*` environment variables (override individual keys). `.env.example` documents
the env-var form if you prefer that for local dev.

## API

Domains can be onboarded, verified, and wired to webhooks entirely over the REST
API using a bearer **API key** (created on the **API keys** screen or via
`POST /v1/api-keys`). See [`docs/API.md`](docs/API.md) for the full flow —
create key → `POST /v1/domains` (returns the DNS records) → publish → `POST
/v1/domains/{id}/verify` (activates) → credentials / mailbox webhooks.

## Layout

Go under `cmd/` + `internal/`, SPA under `web/`, SQL migrations under
`migrations/`.

## Make targets

| Target | Purpose |
|---|---|
| `make all` | build SPA + binary |
| `make run` | run relayd (auto-migrate) |
| `make test` | Go unit/integration tests |
| `make lint` | golangci-lint + tsc |
| `make migrate` / `migrate-down` | apply / roll back migrations |
| `make sqlc` | regenerate typed queries |
| `make web-build` | build the embedded SPA |
| `make e2e` | Playwright suite |

## Operational prerequisites (before first send - Phase 4+)

These are ops requirements, not code. Mail delivery **will fail or be junked**
without them. The exact records to publish are shown in **Settings → Server DNS**
for your configured hostname/IP (the reference deployment is `mail.as135559.net.au`
on `160.30.37.130` / `2001:df4:2040:5::2`):

- **PTR / reverse DNS** for each sending IP **must** resolve to the EHLO hostname,
  and forward DNS must match back.
- **Port 25 egress** confirmed with the upstream/DC (must not be blocked or
  SNAT-rewritten - a common problem on residential/cloud NATs).
- **IP not on major blocklists** (Spamhaus, Barracuda, etc.).
- **Warm-up plan** - ramp volume gradually; monitor Google Postmaster Tools and
  Microsoft SNDS from day one.

## Deployment

Single static binary as a systemd service on a dedicated VM. TLS via Let's Encrypt
(certmagic, HTTP-01) for the server hostname only - hosted domains never need
certs (identity is DKIM/SPF/DMARC). See Phase 7.
