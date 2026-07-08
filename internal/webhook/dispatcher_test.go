package webhook

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"relay/internal/crypto"
	"relay/internal/storage"
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
	conn, _ := st.Pool.Acquire(context.Background())
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_lock(918273645)")
	code := m.Run()
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock(918273645)")
	conn.Release()
	st.Close()
	os.Exit(code)
}

func newSealer(t *testing.T) *crypto.Sealer {
	t.Helper()
	s, err := crypto.NewSealer(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// seed creates a receiving domain, a mailbox (webhook -> url), an inbound
// message + blob, and a pending webhook delivery. Returns the delivery id.
func seed(t *testing.T, sealer *crypto.Sealer, blobs *storage.Store, url, secret, raw string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")
	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{Name: "wh.example", VerifyToken: "t", BounceSubdomain: "bounce.wh.example", Receiving: true})
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := sealer.Seal([]byte(secret))
	mb, err := testStore.CreateMailbox(ctx, store.CreateMailboxParams{DomainID: d.ID, LocalPart: "support", WebhookUrl: url, WebhookSecretEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := blobs.Put([]byte(raw))
	did := d.ID
	msg, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: uuid.New(), Direction: "inbound", DomainID: &did, RcptTo: []string{"support@wh.example"},
		BodyRef: &ref, Status: "received",
	})
	if err != nil {
		t.Fatal(err)
	}
	wd, err := testStore.CreateWebhookDelivery(ctx, store.CreateWebhookDeliveryParams{MailboxID: mb.ID, MessageID: msg.ID})
	if err != nil {
		t.Fatal(err)
	}
	return wd.ID
}

const inboundRaw = "From: alice@example.com\r\nTo: support@wh.example\r\nSubject: Hi\r\n\r\nbody text\r\n"

func TestWebhookDeliverySigned(t *testing.T) {
	sealer := newSealer(t)
	blobs, _ := storage.New(t.TempDir())
	secret := "webhook-secret"

	var got struct {
		sync.Mutex
		body []byte
		sig  string
		ts   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Lock()
		got.body = b
		got.sig = r.Header.Get("X-Relay-Signature")
		got.ts = r.Header.Get("X-Relay-Timestamp")
		got.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	seed(t, sealer, blobs, srv.URL, secret, inboundRaw)
	d := &Dispatcher{Store: testStore, Blobs: blobs, Sealer: sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.Client = srv.Client()
	d.tick(context.Background())

	got.Lock()
	defer got.Unlock()
	if len(got.body) == 0 {
		t.Fatal("webhook server received no request")
	}
	// Signature verifies with the mailbox secret.
	if !Verify([]byte(secret), got.ts, got.body, got.sig) {
		t.Errorf("HMAC signature did not verify: %s", got.sig)
	}
}

func TestWebhookRetryThenDeadLetter(t *testing.T) {
	sealer := newSealer(t)
	blobs, _ := storage.New(t.TempDir())

	// Always-500 server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	id := seed(t, sealer, blobs, srv.URL, "s", inboundRaw)
	d := &Dispatcher{Store: testStore, Blobs: blobs, Sealer: sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.Client = srv.Client()

	// First attempt: 500 → scheduled for retry (still pending, in the future).
	d.tick(context.Background())
	wd, _ := testStore.GetWebhookDelivery(context.Background(), id)
	if wd.Result != "pending" {
		t.Fatalf("after first failure result = %s, want pending", wd.Result)
	}
	if wd.AttemptNo != 1 {
		t.Errorf("attempt_no = %d, want 1", wd.AttemptNo)
	}

	// Simulate the delivery having been created > MaxAge ago so the next failure
	// dead-letters, and make it due now.
	_, _ = testStore.Pool.Exec(context.Background(),
		"UPDATE webhook_deliveries SET created_at = now() - interval '25 hours', next_attempt_at = now() WHERE id = $1", id)
	d.tick(context.Background())
	wd, _ = testStore.GetWebhookDelivery(context.Background(), id)
	if wd.Result != "dead_letter" {
		t.Fatalf("result = %s, want dead_letter", wd.Result)
	}
}

func TestWebhookNowInjectable(t *testing.T) {
	// Guard: nowT override used by tests must not panic.
	orig := nowT
	defer func() { nowT = orig }()
	nowT = func() time.Time { return time.Unix(1700000000, 0) }
	if nowUnix() != 1700000000 {
		t.Fatal("nowUnix override failed")
	}
}
