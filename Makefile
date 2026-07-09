# Relay build/dev targets. Assumes Go on PATH and a local Postgres (see .env.example).
SHELL := /bin/bash
DB_URL ?= postgres://relay:relay_dev_pw@127.0.0.1:5432/relay?sslmode=disable
BIN    := relayd

export PATH := $(PATH):/usr/local/go/bin:$(HOME)/go/bin

.PHONY: all build run test lint migrate migrate-down sqlc web-build web-install e2e clean tidy

all: web-build build

## Build the Go binary (embeds web/dist).
build: web-build
	go build -o $(BIN) ./cmd/relayd

## Run the server (auto-migrates by default).
run: web-build
	RELAY_DATABASE_URL="$(DB_URL)" go run ./cmd/relayd

## Go unit tests.
test:
	RELAY_TEST_DATABASE_URL="postgres://relay:relay_dev_pw@127.0.0.1:5432/relay_test?sslmode=disable" \
		go test ./... -count=1

## Lint.
lint:
	golangci-lint run ./...
	cd web && npm run lint

## Apply / roll back migrations via the CLI.
migrate:
	migrate -path migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path migrations -database "$(DB_URL)" down 1

## Regenerate sqlc code.
sqlc:
	sqlc generate

## Web.
web-install:
	cd web && npm install --no-audit --no-fund

web-build:
	cd web && npm run build

## Playwright E2E: build SPA, start relayd on a test DB, run specs, tear down.
E2E_DB ?= postgres://relay:relay_dev_pw@127.0.0.1:5432/relay_test?sslmode=disable
e2e: web-build build
	@echo "starting relayd for e2e…"
	@PGPASSWORD=relay_dev_pw psql "$(E2E_DB)" -qc "TRUNCATE domains, admin_users, admin_sessions, api_tokens, app_settings CASCADE" 2>/dev/null || true
	@RELAY_HTTP_ADDR=":8090" RELAY_DATABASE_URL="$(E2E_DB)" \
	 RELAY_HOSTNAME="mail.as135559.net.au" \
	 RELAY_SECRET_KEY="ZGV2LXJlbGF5LTMyLWJ5dGUta2V5LWZvci1sb2NhbCE=" \
	 RELAY_ADMIN_TOKENS="relay_dev_token" \
	 RELAY_ADMIN_USER="e2e" RELAY_ADMIN_PASSWORD="e2e-password-123" \
	 RELAY_SUBMISSION_ENABLED="false" RELAY_INBOUND_ENABLED="false" \
	 RELAY_DELIVERY_ENABLED="false" ./$(BIN) & echo $$! > .relayd.e2e.pid
	@sleep 2
	@cd web && BASE_URL="http://localhost:8090" TEST_TOKEN="relay_dev_token" npm run e2e; \
	 status=$$?; kill `cat ../.relayd.e2e.pid` 2>/dev/null; rm -f ../.relayd.e2e.pid; exit $$status

clean:
	rm -f $(BIN)
	rm -rf web/dist

tidy:
	go mod tidy

## Delivery throughput load test (opt-in; needs relay_test DB).
loadtest:
	RELAY_TEST_DATABASE_URL="$(E2E_DB)" RELAY_LOAD_TEST=1 go test ./internal/delivery/ -run Throughput -count=1 -v
