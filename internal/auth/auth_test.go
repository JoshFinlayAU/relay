package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"relay/internal/store"
)

var testStore *store.Store

func TestMain(m *testing.M) {
	url := os.Getenv("RELAY_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://relay:relay_dev_pw@127.0.0.1:5432/relay_test?sslmode=disable"
	}
	if err := store.Migrate(url); err != nil {
		panic("migrate: " + err.Error())
	}
	st, err := store.Connect(context.Background(), url, 5)
	if err != nil {
		panic("connect: " + err.Error())
	}
	testStore = st
	// Serialize with other test packages sharing this DB (they all TRUNCATE).
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

func TestSecretRoundTrip(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifySecret(secret, hash)
	if err != nil || !ok {
		t.Fatalf("verify legit secret: ok=%v err=%v", ok, err)
	}
	ok, _ = VerifySecret("wrong", hash)
	if ok {
		t.Fatal("verify accepted wrong secret")
	}
}

// seedCredential creates a domain + credential with a known secret.
func seedCredential(t *testing.T, secret string) store.Credential {
	t.Helper()
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")
	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{
		Name: "auth-test.example", VerifyToken: "tok", BounceSubdomain: "bounce.auth-test.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	c, err := testStore.CreateCredential(ctx, store.CreateCredentialParams{
		DomainID: d.ID, Username: "app@auth-test.example", SecretHash: hash, Restrictions: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAuthenticateSuccessAndFailure(t *testing.T) {
	secret := "s3cr3t-value"
	seedCredential(t, secret)
	a := NewAuthenticator(testStore, DefaultConfig())

	c, err := a.Authenticate(context.Background(), "app@auth-test.example", secret, "1.2.3.4")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if c.Username != "app@auth-test.example" {
		t.Errorf("wrong credential returned")
	}

	// Wrong secret.
	if _, err := a.Authenticate(context.Background(), "app@auth-test.example", "nope", "1.2.3.4"); err != ErrAuthFailed {
		t.Errorf("wrong secret err = %v, want ErrAuthFailed", err)
	}
	// Unknown user (no enumeration - same error).
	if _, err := a.Authenticate(context.Background(), "ghost@auth-test.example", "x", "9.9.9.9"); err != ErrAuthFailed {
		t.Errorf("unknown user err = %v, want ErrAuthFailed", err)
	}
}

func TestLockoutAfterMaxFailures(t *testing.T) {
	secret := "correct-secret"
	seedCredential(t, secret)
	cfg := DefaultConfig()
	cfg.MaxFailures = 3
	cfg.RateMax = 1000 // disable rate limiter for this test
	a := NewAuthenticator(testStore, cfg)

	// 3 bad attempts → credential locked.
	for i := 0; i < 3; i++ {
		if _, err := a.Authenticate(context.Background(), "app@auth-test.example", "bad", "5.5.5.5"); err != ErrAuthFailed {
			t.Fatalf("attempt %d err = %v", i, err)
		}
	}
	// Even the CORRECT secret is now rejected due to lock.
	_, err := a.Authenticate(context.Background(), "app@auth-test.example", secret, "5.5.5.5")
	if err != ErrLocked {
		t.Fatalf("after lockout err = %v, want ErrLocked", err)
	}

	// Simulate lock expiry by advancing the authenticator clock.
	a.now = func() time.Time { return time.Now().Add(20 * time.Minute) }
	c, err := a.Authenticate(context.Background(), "app@auth-test.example", secret, "5.5.5.5")
	if err != nil {
		t.Fatalf("after lock expiry err = %v, want success", err)
	}
	if c == nil {
		t.Fatal("expected credential after lock expiry")
	}
}

func TestRateLimiting(t *testing.T) {
	seedCredential(t, "secret")
	cfg := DefaultConfig()
	cfg.RateMax = 3
	cfg.MaxFailures = 1000 // don't lock; isolate rate-limit behaviour
	a := NewAuthenticator(testStore, cfg)

	ip := "7.7.7.7"
	for i := 0; i < 3; i++ {
		_, _ = a.Authenticate(context.Background(), "app@auth-test.example", "bad", ip)
	}
	// Next attempt from same IP is rate-limited before hitting the DB.
	if _, err := a.Authenticate(context.Background(), "app@auth-test.example", "bad", ip); err != ErrRateLimited {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestSuspendedCredentialRejected(t *testing.T) {
	secret := "sec"
	c := seedCredential(t, secret)
	_, _ = testStore.UpdateCredentialStatus(context.Background(), store.UpdateCredentialStatusParams{ID: c.ID, Status: "suspended"})
	a := NewAuthenticator(testStore, DefaultConfig())
	if _, err := a.Authenticate(context.Background(), "app@auth-test.example", secret, "8.8.8.8"); err != ErrNotActive {
		t.Fatalf("suspended err = %v, want ErrNotActive", err)
	}
}
