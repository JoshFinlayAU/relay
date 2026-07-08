#!/usr/bin/env bash
#
# Relay one-shot installer.
#
#   git clone <repo> && cd relay && ./install.sh
#
# Installs prerequisites (Go, Node.js, PostgreSQL), provisions the database,
# generates a .env with fresh secrets, builds the SPA + binary, and prints how
# to run it. Idempotent - safe to re-run. Debian/Ubuntu (apt) for the package
# steps; on other distros it skips installs and just builds if the tools exist.
#
# Secrets (DB password, secret key, admin password, API token) are written to a
# 0600 relay.toml and read via RELAY_CONFIG - never passed on the command line,
# so they never appear in `ps` or the process environment.
#
# Options:
#   --no-service     don't install the systemd unit (build + config only, for dev)
#   --with-test-db   also create the relay_test database (for `make test`)
#   -h | --help      show this help
#
# Overridable via environment (or an existing relay.toml - that always wins):
#   RELAY_HOSTNAME        default: this machine's hostname -f
#   RELAY_HTTP_ADDR       default: :8080
#   DB_NAME / DB_USER     default: relay / relay
#   DB_PASSWORD           default: generated (or relay_dev_pw if DB already exists)
#
set -euo pipefail

# ── pretty logging ──────────────────────────────────────────────────────────
if [[ -t 1 ]]; then B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; C=$'\033[36m'; N=$'\033[0m'
else B=""; G=""; Y=""; R=""; C=""; N=""; fi
step() { echo -e "\n${B}${C}==>${N} ${B}$*${N}"; }
info() { echo -e "    $*"; }
ok()   { echo -e "    ${G}✓${N} $*"; }
warn() { echo -e "    ${Y}!${N} $*"; }
die()  { echo -e "\n${R}✗ $*${N}" >&2; exit 1; }

# ── args ────────────────────────────────────────────────────────────────────
INSTALL_SERVICE=true
WITH_TEST_DB=false
for arg in "$@"; do
  case "$arg" in
    --no-service)   INSTALL_SERVICE=false ;;
    --service)      INSTALL_SERVICE=true ;;  # accepted for compatibility (now the default)
    --with-test-db) WITH_TEST_DB=true ;;
    -h|--help)      sed -n '2,34p' "$0" | sed 's/^#\s\?//'; exit 0 ;;
    *)              die "unknown option: $arg (try --help)" ;;
  esac
done

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_DIR"
[[ -f cmd/relayd/main.go ]] || die "run this from the Relay repo root (cmd/relayd/main.go not found)"

# sudo helper (no-op if already root)
SUDO=""
if [[ "$(id -u)" -ne 0 ]]; then command -v sudo >/dev/null 2>&1 && SUDO="sudo" || die "not root and sudo not found"; fi

HAVE_APT=false; command -v apt-get >/dev/null 2>&1 && HAVE_APT=true
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"

# Read a scalar string value from relay.toml (e.g. toml_str admin_user).
toml_str() { grep -E "^\s*$1\s*=" relay.toml 2>/dev/null | tail -n1 | sed -E 's/^[^=]*=\s*//; s/^"//; s/"\s*$//'; }
# First entry of the admin_tokens = ["..."] array.
toml_first_token() { grep -E '^\s*admin_tokens\s*=' relay.toml 2>/dev/null | sed -E 's/.*\[\s*"?([^",]*)"?.*/\1/'; }

# Minimum versions.
GO_MIN=1.23; GO_INSTALL=1.25.11; NODE_MIN=18

ver_ge() { [[ "$(printf '%s\n%s' "$2" "$1" | sort -V | head -n1)" == "$2" ]]; }

# ── 1. Go ─────────────────────────────────────────────────────────────────--
step "Checking Go (>= $GO_MIN)"
if command -v go >/dev/null 2>&1 && ver_ge "$(go env GOVERSION | sed 's/go//')" "$GO_MIN"; then
  ok "go $(go env GOVERSION) present"
else
  warn "installing Go $GO_INSTALL"
  arch=$(uname -m); case "$arch" in x86_64) garch=amd64;; aarch64|arm64) garch=arm64;; *) die "unsupported arch $arch";; esac
  tarball="go${GO_INSTALL}.linux-${garch}.tar.gz"
  tmp=$(mktemp -d)
  curl -fsSL "https://go.dev/dl/${tarball}" -o "$tmp/$tarball" || die "download Go failed"
  $SUDO rm -rf /usr/local/go && $SUDO tar -C /usr/local -xzf "$tmp/$tarball"
  rm -rf "$tmp"
  ok "installed Go $GO_INSTALL to /usr/local/go"
fi

# ── 2. Node.js ────────────────────────────────────────────────────────────--
step "Checking Node.js (>= $NODE_MIN)"
if command -v node >/dev/null 2>&1 && ver_ge "$(node -v | sed 's/v//')" "$NODE_MIN"; then
  ok "node $(node -v) present"
elif $HAVE_APT; then
  warn "installing Node.js via apt"
  $SUDO apt-get update -qq
  $SUDO apt-get install -y -qq nodejs npm >/dev/null
  command -v node >/dev/null 2>&1 && ver_ge "$(node -v | sed 's/v//')" "$NODE_MIN" \
    || die "apt Node.js is < $NODE_MIN; install Node 20+ (e.g. https://github.com/nodesource/distributions) and re-run"
  ok "node $(node -v) installed"
else
  die "Node.js >= $NODE_MIN required and no apt available"
fi

# ── 3. PostgreSQL ─────────────────────────────────────────────────────────--
step "Checking PostgreSQL"
if ! command -v psql >/dev/null 2>&1; then
  $HAVE_APT || die "PostgreSQL not found and no apt available"
  warn "installing PostgreSQL"
  $SUDO apt-get update -qq
  $SUDO apt-get install -y -qq postgresql postgresql-client >/dev/null
  ok "PostgreSQL installed"
fi
# Make sure the server is up.
if ! $SUDO pg_isready -q 2>/dev/null; then
  if command -v systemctl >/dev/null 2>&1; then $SUDO systemctl enable --now postgresql >/dev/null 2>&1 || true; fi
  # Fallback for containers without systemd.
  if ! $SUDO pg_isready -q 2>/dev/null && command -v pg_ctlcluster >/dev/null 2>&1; then
    cl=$(pg_lsclusters -h | awk 'NR==1{print $1" "$2}'); [[ -n "$cl" ]] && $SUDO pg_ctlcluster $cl start 2>/dev/null || true
  fi
fi
$SUDO pg_isready -q 2>/dev/null && ok "PostgreSQL is running" || warn "could not confirm PostgreSQL is running (continuing)"

# Run a psql command as the 'postgres' superuser (peer auth on the local socket):
# as root switch users with `su`; otherwise use `sudo -u`.
psql_su() {
  if [[ "$(id -u)" -eq 0 ]]; then
    su -s /bin/sh postgres -c "cd /tmp && psql -tAqc \"$1\""
  else
    sudo -u postgres psql -tAqc "$1"
  fi
}

# ── 4. Database + role ────────────────────────────────────────────────────--
step "Provisioning database"
# Fail loudly (not silently under set -e) if we can't reach PostgreSQL as superuser.
if ! pg_err="$(psql_su "SELECT 1" 2>&1)"; then
  die "cannot connect to PostgreSQL as the 'postgres' superuser:\n    ${pg_err}\n    Ensure PostgreSQL is running and reachable on the local socket."
fi
DB_NAME="${DB_NAME:-relay}"
DB_USER="${DB_USER:-relay}"
# If a relay.toml already exists, its database_url is the source of truth.
EXISTING_URL=""
[[ -f relay.toml ]] && EXISTING_URL="$(grep -E '^\s*database_url\s*=' relay.toml | tail -n1 | sed -E 's/.*=\s*"?([^"]*)"?.*/\1/' || true)"

role_exists=$(psql_su "SELECT 1 FROM pg_roles WHERE rolname='$DB_USER'" 2>/dev/null || true)
if [[ "$role_exists" == "1" ]]; then
  ok "role '$DB_USER' exists"
  # Reuse the password already baked into .env; else default dev password.
  if [[ "$EXISTING_URL" =~ ://$DB_USER:([^@]+)@ ]]; then DB_PASSWORD="${BASH_REMATCH[1]}"
  else DB_PASSWORD="${DB_PASSWORD:-relay_dev_pw}"; fi
else
  DB_PASSWORD="${DB_PASSWORD:-$(head -c18 /dev/urandom | base64 | tr -d '/+=' | head -c24)}"
  psql_su "CREATE ROLE $DB_USER LOGIN PASSWORD '$DB_PASSWORD'" >/dev/null
  ok "created role '$DB_USER'"
fi
if [[ "$(psql_su "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" 2>/dev/null || true)" == "1" ]]; then
  ok "database '$DB_NAME' exists"
else
  psql_su "CREATE DATABASE $DB_NAME OWNER $DB_USER" >/dev/null && ok "created database '$DB_NAME'"
fi
if $WITH_TEST_DB && [[ "$(psql_su "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}_test'" 2>/dev/null || true)" != "1" ]]; then
  psql_su "CREATE DATABASE ${DB_NAME}_test OWNER $DB_USER" >/dev/null && ok "created database '${DB_NAME}_test'"
fi
DB_URL="postgres://$DB_USER:$DB_PASSWORD@127.0.0.1:5432/$DB_NAME?sslmode=disable"

# ── 5. relay.toml (only if missing) ───────────────────────────────────────--
step "Configuration (relay.toml)"
CONFIG_FILE="$REPO_DIR/relay.toml"
ADMIN_PASSWORD=""; ADMIN_TOKEN=""; ADMIN_USER="${RELAY_ADMIN_USER:-admin}"
HTTP_ADDR="${RELAY_HTTP_ADDR:-:8080}"
if [[ -f relay.toml ]]; then
  ok "relay.toml already exists - leaving it untouched"
  HTTP_ADDR="$(grep -E '^\s*http_addr\s*=' relay.toml | tail -n1 | sed -E 's/.*=\s*"?([^"]*)"?.*/\1/' || echo "$HTTP_ADDR")"
else
  SECRET_KEY="$(head -c32 /dev/urandom | base64)"
  ADMIN_PASSWORD="$(head -c18 /dev/urandom | base64 | tr -d '/+=' | head -c20)"
  ADMIN_TOKEN="relay_$(head -c24 /dev/urandom | base64 | tr -d '/+=' | head -c32)"
  HOSTNAME_DEFAULT="${RELAY_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"
  umask 077
  cat > relay.toml <<TOML
# Relay configuration - generated by install.sh. Mode 0600; keep secrets here,
# not on the command line. See relay.toml.example for all options.
hostname         = "$HOSTNAME_DEFAULT"
http_addr        = "$HTTP_ADDR"
submission_addr  = ":587"
submissions_addr = ":465"
inbound_addr     = ":25"

database_url = "$DB_URL"
max_conns    = 10
auto_migrate = true

secret_key     = "$SECRET_KEY"
admin_tokens   = ["$ADMIN_TOKEN"]
admin_user     = "$ADMIN_USER"
admin_password = "$ADMIN_PASSWORD"

storage_dir = "storage"
log_level   = "info"

# Set true + use :443/:80 for public Let's Encrypt TLS (see relay.toml.example).
tls_enabled    = false
acme_email     = "ops@$HOSTNAME_DEFAULT"
acme_http_addr = ":80"
TOML
  umask 022
  chmod 600 relay.toml
  ok "generated relay.toml (0600) with a fresh secret key, admin password, and API token"
fi

# ── 6. Build ────────────────────────────────────────────────────────────────
step "Building the WebUI + binary (this can take a minute)"
info "installing web dependencies…"
( cd web && npm ci --no-audit --no-fund >/dev/null 2>&1 || npm install --no-audit --no-fund >/dev/null 2>&1 )
ok "web dependencies installed"
info "building SPA + Go binary…"
make build >/dev/null
[[ -x ./relayd ]] || die "build failed (no relayd binary produced)"
ok "built ./relayd ($(du -h relayd | cut -f1))"

# ── 7. systemd service ─────────────────────────────────────────────────────
SERVICE_ACTIVE=false
if $INSTALL_SERVICE; then
  if ! command -v systemctl >/dev/null 2>&1; then
    warn "systemd not available - skipping service install (run manually, see below)"
    INSTALL_SERVICE=false
  else
    step "Installing systemd service"
    RUN_USER="${SUDO_USER:-$(id -un)}"
    unit=/etc/systemd/system/relayd.service
    # Secrets live in relay.toml (0600) and are read via RELAY_CONFIG - nothing
    # sensitive is placed in the unit, argv, or the process environment.
    $SUDO tee "$unit" >/dev/null <<UNIT
[Unit]
Description=Relay transactional mail server
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=$RUN_USER
WorkingDirectory=$REPO_DIR
Environment=RELAY_CONFIG=$CONFIG_FILE
ExecStart=$REPO_DIR/relayd
Restart=on-failure
RestartSec=3
# Ports < 1024 (25/80/443/465/587) need this when not running as root:
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
UNIT
    ok "wrote $unit (reads $CONFIG_FILE)"
    # Guard every systemctl call with `|| true` so a start failure can't abort
    # the script (set -e) before we report status + logs.
    $SUDO systemctl daemon-reload || true
    $SUDO systemctl enable relayd >/dev/null 2>&1 || true
    $SUDO systemctl restart relayd >/dev/null 2>&1 || true
    # First boot runs migrations (and ACME if TLS is on) - give it a few seconds.
    for _ in 1 2 3 4 5 6 7 8; do
      $SUDO systemctl is-active --quiet relayd && break
      sleep 1
    done
    if $SUDO systemctl is-active --quiet relayd; then
      ok "relayd.service is running"
      SERVICE_ACTIVE=true
    else
      warn "relayd.service did not become active - recent logs:"
      $SUDO journalctl -u relayd -n 15 --no-pager 2>/dev/null | sed 's/^/      /' || true
      warn "fix the above, then: ${SUDO:+sudo }systemctl restart relayd"
    fi
  fi
fi

# ── done ────────────────────────────────────────────────────────────────────
# Build the URL from scheme (TLS?) + host + port.
TLS_ON="$(toml_str tls_enabled)"
HOST_CFG="$(toml_str hostname)"; HOST_CFG="${HOST_CFG:-localhost}"
PORT="${HTTP_ADDR#:}"
if [[ "$TLS_ON" == "true" ]]; then
  [[ "$PORT" == "443" ]] && URL="https://$HOST_CFG" || URL="https://$HOST_CFG:$PORT"
else
  [[ "$PORT" == "80" ]] && URL="http://localhost" || URL="http://localhost:$PORT"
fi
echo -e "\n${B}${G}✓ Relay is installed.${N}"
if $SERVICE_ACTIVE; then
  echo -e "\n${B}Service:${N} relayd.service is enabled and running (reads $CONFIG_FILE)."
  echo -e "    logs:    ${C}journalctl -u relayd -f${N}"
  echo -e "    restart: ${C}sudo systemctl restart relayd${N}"
elif $INSTALL_SERVICE; then
  echo -e "\n${B}Service:${N} installed but not active - check ${C}journalctl -u relayd -e${N}."
else
  echo -e "\n${B}Run it (secrets read from the config file, not the command line):${N}"
  echo -e "    ${C}RELAY_CONFIG=./relay.toml ./relayd${N}"
fi
echo -e "\n${B}Then open:${N} $URL"
# Always show login details. Freshly generated ones are in memory; on a re-run
# read them back from relay.toml so the operator is never left guessing.
FROM_FILE=""
if [[ -z "$ADMIN_PASSWORD" && -f relay.toml ]]; then
  ADMIN_USER="$(toml_str admin_user)"
  ADMIN_PASSWORD="$(toml_str admin_password)"
  ADMIN_TOKEN="$(toml_first_token)"
  FROM_FILE=1
fi
if [[ -n "$ADMIN_PASSWORD" ]]; then
  echo -e "\n${B}Login${N} (also stored in ${C}relay.toml${N}):"
  echo -e "    user:      ${C}${ADMIN_USER}${N}"
  echo -e "    password:  ${C}${ADMIN_PASSWORD}${N}"
  echo -e "    API token: ${C}${ADMIN_TOKEN}${N}"
  if [[ -n "$FROM_FILE" ]]; then
    echo -e "    ${Y}(from relay.toml - if you changed the password in the UI, use that instead.)${N}"
  else
    echo -e "    ${Y}Change the password from the Admin Users screen after first login.${N}"
  fi
else
  echo -e "\n    Credentials are in ${C}relay.toml${N} (admin_user / admin_password / admin_tokens)."
fi
echo -e "\n${B}Notes:${N}"
echo -e "    • Secrets live in ${C}relay.toml${N} (mode 0600) - never in ps/env. Schema auto-migrates on boot."
echo -e "    • For public TLS + real mail sending, set tls_enabled=true and use :443/:587/:465/:25 in"
echo -e "      relay.toml, and review the ops prerequisites (PTR, port 25, blocklists) in README.md."
