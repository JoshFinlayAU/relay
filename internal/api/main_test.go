package api

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"relay/internal/crypto"
	"relay/internal/dns"
	"relay/internal/storage"
	"relay/internal/store"
)

var testStore *store.Store

const testToken = "test_token_123"

func TestMain(m *testing.M) {
	url := os.Getenv("RELAY_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://relay:relay_dev_pw@127.0.0.1:5432/relay_test?sslmode=disable"
	}
	if err := store.Migrate(url); err != nil {
		panic("migrate test db: " + err.Error())
	}
	st, err := store.Connect(context.Background(), url, 5)
	if err != nil {
		panic("connect test db: " + err.Error())
	}
	testStore = st
	unlock := lockTestDB(st)
	code := m.Run()
	unlock()
	st.Close()
	os.Exit(code)
}

// lockTestDB takes a Postgres advisory lock so packages sharing relay_test run
// their TRUNCATE-based tests one at a time (Go runs packages in parallel).
func lockTestDB(st *store.Store) func() {
	ctx := context.Background()
	conn, err := st.Pool.Acquire(ctx)
	if err != nil {
		panic(err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(918273645)"); err != nil {
		panic(err)
	}
	return func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock(918273645)")
		conn.Release()
	}
}

// newTestServer returns an httptest server with a clean database.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	// Clean slate; CASCADE clears dependent rows.
	_, err := testStore.Pool.Exec(context.Background(),
		`TRUNCATE domains, credentials, mailboxes, messages, events, admin_users, admin_sessions RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}

	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	sealer, err := crypto.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	params := dns.Params{
		Hostname:   "mail.as135559.net.au",
		SPFInclude: "spf.mail.as135559.net.au",
		DMARCRua:   "mailto:dmarc@as135559.net.au",
	}
	blobs, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		Store:       testStore,
		Log:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Tokens:      []string{testToken},
		Sealer:      sealer,
		Verifier:    dns.NewVerifier(params, nil),
		Params:      params,
		SendingIPv4: "160.30.37.130",
		SendingIPv6: "2001:df4:2040:5::2",
		Blobs:       blobs,
		Hostname:    "mail.as135559.net.au",
	}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts
}
