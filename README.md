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

Options: `./install.sh --no-service` skips the systemd unit and just builds +
configures (run it yourself with `RELAY_CONFIG=./relay.toml ./relayd`);
`--with-test-db` also creates the `relay_test` database for `make test`.
Debian/Ubuntu for the package steps; on other distros it skips the installs and
just builds if Go/Node/Postgres are already present.

Health: `curl localhost:8080/healthz` · Metrics: authenticated `curl -H "Authorization: Bearer <token>" localhost:8080/metrics`

### Manual (dev)

```bash
make all                              # build SPA + binary
cp relay.toml.example relay.toml       # fill in secret_key, database_url, admin_* ; chmod 600
RELAY_CONFIG=./relay.toml ./relayd     # auto-applies migrations
```

Config precedence is: built-in defaults → `relay.toml` (path in `RELAY_CONFIG`) →
`RELAY_*` environment variables (override individual keys). `.env.example` documents
the env-var form if you prefer that for local dev.

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
without them:

- **PTR / reverse DNS** for the sending IP (160.30.37.130, `2001:df4:2040:5::2`)
  **must** resolve to the EHLO hostname `mail.as135559.net.au`, and forward DNS
  must match back.
- **Port 25 egress** confirmed with the upstream/DC (verified OPEN on this host).
- **IP not on major blocklists** (Spamhaus, Barracuda, etc.).
- **Warm-up plan** - ramp volume gradually; monitor Google Postmaster Tools and
  Microsoft SNDS from day one.

## Deployment

Single static binary as a systemd service on a dedicated VM. TLS via Let's Encrypt
(certmagic, HTTP-01) for the server hostname only - hosted domains never need
certs (identity is DKIM/SPF/DMARC). See Phase 7.
