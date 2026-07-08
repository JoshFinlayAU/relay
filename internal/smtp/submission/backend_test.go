package submission

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"relay/internal/auth"
	"relay/internal/certs"
	"relay/internal/crypto"
	"relay/internal/dkim"
	"relay/internal/storage"
	"relay/internal/store"
)

var testStore *store.Store

const (
	testHost   = "mail.test"
	testSecret = "submission-secret"
)

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
	// Serialize with other packages sharing relay_test.
	conn, _ := st.Pool.Acquire(context.Background())
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_lock(918273645)")
	code := m.Run()
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock(918273645)")
	conn.Release()
	st.Close()
	os.Exit(code)
}

// harness bundles a running submission server and its collaborators.
type harness struct {
	addr    string
	sealer  *crypto.Sealer
	blobs   *storage.Store
	dkimTXT string
	domain  string
	cleanup func()
}

func setup(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	_, err := testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")
	if err != nil {
		t.Fatal(err)
	}

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	sealer, err := crypto.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	domain := "voxsub.example"

	// Domain (active) + DKIM key + credential.
	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{
		Name: domain, VerifyToken: "t", BounceSubdomain: "bounce." + domain,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = testStore.UpdateDomainStatus(ctx, store.UpdateDomainStatusParams{ID: d.ID, Status: "active"})

	kp, err := dkim.Generate("rly2026a")
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := sealer.Seal(kp.PrivatePEM)
	if _, err := testStore.InsertDKIMKey(ctx, store.InsertDKIMKeyParams{
		DomainID: d.ID, Selector: kp.Selector, Algorithm: "rsa", PrivateKeyEnc: enc, PublicKey: kp.PublicB64,
	}); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashSecret(testSecret)
	if _, err := testStore.CreateCredential(ctx, store.CreateCredentialParams{
		DomainID: d.ID, Username: "app@" + domain, SecretHash: hash, Restrictions: []byte("{}"),
	}); err != nil {
		t.Fatal(err)
	}

	blobs, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := New(Deps{
		Store: testStore, Auth: auth.NewAuthenticator(testStore, auth.DefaultConfig()),
		Sealer: sealer, Blobs: blobs, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hostname: testHost, MaxMessageBytes: 1 << 20,
	})

	cert, err := certs.SelfSigned(testHost)
	if err != nil {
		t.Fatal(err)
	}
	tlsConf := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv := NewServer("", testHost, backend, tlsConf, 1<<20)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	return &harness{
		addr:    ln.Addr().String(),
		sealer:  sealer,
		blobs:   blobs,
		dkimTXT: dkim.TXTValue(kp.PublicB64),
		domain:  domain,
		cleanup: func() { _ = srv.Close(); _ = ln.Close() },
	}
}

// dialAuthed connects, does STARTTLS, and (optionally) authenticates.
func dialAuthed(t *testing.T, h *harness, authenticate bool) *smtp.Client {
	t.Helper()
	c, err := smtp.DialStartTLS(h.addr, &tls.Config{ServerName: testHost, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("dial+starttls: %v", err)
	}
	if authenticate {
		if err := c.Auth(sasl.NewPlainClient("", "app@"+h.domain, testSecret)); err != nil {
			t.Fatalf("auth: %v", err)
		}
	}
	return c
}

func testMessage(from string) string {
	msg := fmt.Sprintf(
		"From: %s\nTo: dest@example.net\nSubject: Hello\nMIME-Version: 1.0\n"+
			"Content-Type: text/plain\n\nThis is a test.\n", from)
	return strings.ReplaceAll(msg, "\n", "\r\n")
}

func send(t *testing.T, c *smtp.Client, from, to, msg string) error {
	t.Helper()
	if err := c.Mail(from, nil); err != nil {
		return err
	}
	if err := c.Rcpt(to, nil); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, msg); err != nil {
		return err
	}
	return w.Close()
}

func TestSubmitSignAndQueue(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	c := dialAuthed(t, h, true)
	defer func() { _ = c.Quit() }()

	from := "app@" + h.domain
	if err := send(t, c, from, "dest@example.net", testMessage(from)); err != nil {
		t.Fatalf("send: %v", err)
	}

	// A queued message row with per-recipient job and a body on disk.
	var msgID, bodyRef, status, mailFrom string
	err := testStore.Pool.QueryRow(context.Background(),
		`SELECT id, body_ref, status, mail_from FROM messages WHERE direction='outbound' LIMIT 1`).
		Scan(&msgID, &bodyRef, &status, &mailFrom)
	if err != nil {
		t.Fatalf("no message row: %v", err)
	}
	if status != "queued" {
		t.Errorf("status = %q, want queued", status)
	}
	if !strings.HasPrefix(mailFrom, "bounce-") || !strings.HasSuffix(mailFrom, "@bounce."+h.domain) {
		t.Errorf("VERP mail_from wrong: %q", mailFrom)
	}
	var jobs int
	_ = testStore.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM delivery_jobs WHERE message_id=$1`, msgID).Scan(&jobs)
	if jobs != 1 {
		t.Errorf("delivery jobs = %d, want 1", jobs)
	}

	// Body on disk and DKIM verifies offline against the domain key.
	signed, err := h.blobs.Get(bodyRef)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(signed), "DKIM-Signature:") {
		t.Fatal("no DKIM-Signature header in stored body")
	}
	if !strings.Contains(string(signed), "Received: from") {
		t.Error("our Received header missing")
	}
	if !strings.Contains(string(signed), "Message-ID:") {
		t.Error("Message-ID not injected")
	}
	verifs, err := dkim.VerifyWithKey(signed, h.dkimTXT)
	if err != nil {
		t.Fatalf("dkim verify: %v", err)
	}
	if len(verifs) != 1 || verifs[0].Err != nil {
		t.Fatalf("DKIM did not verify: %+v", verifs)
	}
}

func TestWrongDomainFromRejected(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	c := dialAuthed(t, h, true)
	defer func() { _ = c.Quit() }()

	// MAIL FROM a domain the credential does not cover → 550.
	err := c.Mail("app@notmine.example", nil)
	if err == nil {
		t.Fatal("expected rejection for wrong-domain MAIL FROM")
	}
	if se, ok := err.(*smtp.SMTPError); ok {
		if se.Code != 550 {
			t.Errorf("code = %d, want 550", se.Code)
		}
	} else {
		t.Errorf("expected SMTPError, got %T: %v", err, err)
	}
}

func TestHeaderFromMismatchRejected(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	c := dialAuthed(t, h, true)
	defer func() { _ = c.Quit() }()

	// Envelope covered, but header From is a different (uncovered) domain → 550.
	from := "app@" + h.domain
	err := send(t, c, from, "dest@example.net", testMessage("someone@evil.example"))
	if err == nil {
		t.Fatal("expected rejection for header From mismatch")
	}
	if se, ok := err.(*smtp.SMTPError); ok && se.Code != 550 {
		t.Errorf("code = %d, want 550", se.Code)
	}
}

func TestOpenRelayRejected(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	// Connect + STARTTLS but do NOT authenticate.
	c := dialAuthed(t, h, false)
	defer c.Close()

	err := c.Mail("app@"+h.domain, nil)
	if err == nil {
		t.Fatal("unauthenticated MAIL FROM must be rejected (open relay!)")
	}
	if se, ok := err.(*smtp.SMTPError); ok && se.Code != 530 {
		t.Errorf("code = %d, want 530", se.Code)
	}
}

func TestBadPasswordRejected(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	c, err := smtp.DialStartTLS(h.addr, &tls.Config{ServerName: testHost, InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Auth(sasl.NewPlainClient("", "app@"+h.domain, "wrong")); err == nil {
		t.Fatal("expected auth failure with wrong password")
	}
}

func TestSuppressedRecipientRejected(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	ctx := context.Background()

	// Suppress the recipient for the sending domain.
	d, _ := testStore.GetDomainByName(ctx, h.domain)
	reason := "hard bounce 5.1.1"
	if _, err := testStore.AddSuppression(ctx, store.AddSuppressionParams{DomainID: d.ID, Address: "dest@example.net", Reason: &reason}); err != nil {
		t.Fatal(err)
	}

	c := dialAuthed(t, h, true)
	defer func() { _ = c.Quit() }()
	from := "app@" + h.domain
	err := send(t, c, from, "dest@example.net", testMessage(from))
	if err == nil {
		t.Fatal("suppressed recipient should be rejected")
	}
	if se, ok := err.(*smtp.SMTPError); ok {
		if se.Code != 550 || se.EnhancedCode != (smtp.EnhancedCode{5, 1, 1}) {
			t.Errorf("code = %d %v, want 550 5.1.1", se.Code, se.EnhancedCode)
		}
	} else {
		t.Errorf("expected SMTPError, got %T", err)
	}
}
