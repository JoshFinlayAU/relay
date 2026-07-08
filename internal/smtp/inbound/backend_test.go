package inbound

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/emersion/go-smtp"
	"github.com/google/uuid"

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

type harness struct {
	addr    string
	domain  string
	msgID   uuid.UUID
	rcpt    string
	cleanup func()
}

func setup(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")
	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{
		Name: "bnc.example", VerifyToken: "t", BounceSubdomain: "bounce.bnc.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = testStore.UpdateDomainStatus(ctx, store.UpdateDomainStatusParams{ID: d.ID, Status: "active"})

	msgID := uuid.New()
	did := d.ID
	verp := "bounce-" + msgID.String() + "@bounce.bnc.example"
	rcpt := "nobody@example.net"
	if _, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: msgID, Direction: "outbound", DomainID: &did, MailFrom: &verp,
		RcptTo: []string{rcpt}, Status: "queued", VerpToken: strp(msgID.String()),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.EnqueueDeliveryJob(ctx, store.EnqueueDeliveryJobParams{MessageID: msgID, Rcpt: rcpt}); err != nil {
		t.Fatal(err)
	}

	blobs, _ := storage.New(t.TempDir())
	b := New(Deps{Store: testStore, Blobs: blobs, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Hostname: "mail.test", MaxMessageBytes: 1 << 20})
	srv := NewServer("", "mail.test", b, nil, 1<<20)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	return &harness{addr: ln.Addr().String(), domain: d.Name, msgID: msgID, rcpt: rcpt,
		cleanup: func() { _ = srv.Close(); _ = ln.Close() }}
}

func strp(s string) *string { return &s }

func deliverBounce(t *testing.T, addr, from, to, body string) error {
	t.Helper()
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Hello("mx.example.com"); err != nil {
		return err
	}
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
	if _, err := io.WriteString(w, body); err != nil {
		return err
	}
	return w.Close()
}

const hardBounce = "From: MAILER-DAEMON\r\nContent-Type: multipart/report; report-type=delivery-status; boundary=B\r\n\r\n" +
	"--B\r\nContent-Type: text/plain\r\n\r\nfailed\r\n\r\n" +
	"--B\r\nContent-Type: message/delivery-status\r\n\r\n" +
	"Final-Recipient: rfc822; nobody@example.net\r\nAction: failed\r\nStatus: 5.1.1\r\n" +
	"Diagnostic-Code: smtp; 550 5.1.1 user unknown\r\n\r\n--B--\r\n"

func TestInboundHardBounceSuppresses(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	ctx := context.Background()

	verp := "bounce-" + h.msgID.String() + "@bounce.bnc.example"
	if err := deliverBounce(t, h.addr, "", verp, hardBounce); err != nil {
		t.Fatalf("deliver bounce: %v", err)
	}

	// Bounce event recorded as hard.
	events, err := testStore.ListBounceEvents(ctx, &h.msgID)
	if err != nil || len(events) != 1 {
		t.Fatalf("bounce events = %v (err %v)", len(events), err)
	}
	if events[0].Type != "hard" {
		t.Errorf("bounce type = %s, want hard", events[0].Type)
	}

	// Address suppressed for the domain.
	d, _ := testStore.GetDomainByName(ctx, "bnc.example")
	sup, _ := testStore.IsSuppressed(ctx, store.IsSuppressedParams{DomainID: d.ID, Address: h.rcpt})
	if !sup {
		t.Error("recipient should be suppressed after hard bounce")
	}

	// Message rolled up to bounced.
	msg, _ := testStore.GetMessage(ctx, h.msgID)
	if msg.Status != "bounced" {
		t.Errorf("message status = %s, want bounced", msg.Status)
	}
}

func TestInboundRejectsNonBounceRecipient(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	// A normal address (not a VERP bounce) must be rejected - no open relay.
	err := deliverBounce(t, h.addr, "spammer@evil.com", "victim@example.net", "From: x\r\n\r\nrelay me\r\n")
	if err == nil {
		t.Fatal("non-bounce recipient must be rejected (open relay!)")
	}
	if se, ok := err.(*smtp.SMTPError); ok && se.Code != 550 {
		t.Errorf("code = %d, want 550", se.Code)
	}
}

func TestInboundRejectsUnknownBounceRef(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	// Valid VERP shape but unknown message id → 550 5.1.1.
	verp := "bounce-" + uuid.New().String() + "@bounce.bnc.example"
	err := deliverBounce(t, h.addr, "", verp, hardBounce)
	if err == nil {
		t.Fatal("unknown bounce ref should be rejected")
	}
	if se, ok := err.(*smtp.SMTPError); ok && se.Code != 550 {
		t.Errorf("code = %d, want 550", se.Code)
	}
}

func TestInboundMailboxDeliveryEnqueuesWebhook(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	ctx := context.Background()

	// Enable receiving + add a mailbox on the domain.
	d, _ := testStore.GetDomainByName(ctx, "bnc.example")
	_, _ = testStore.SetDomainReceiving(ctx, store.SetDomainReceivingParams{ID: d.ID, Receiving: true})
	if _, err := testStore.CreateMailbox(ctx, store.CreateMailboxParams{
		DomainID: d.ID, LocalPart: "support", WebhookUrl: "https://example.test/hook", WebhookSecretEnc: []byte("x"),
	}); err != nil {
		t.Fatal(err)
	}

	msg := "From: sender@external.example\r\nTo: support@bnc.example\r\nSubject: Help\r\n\r\nhi\r\n"
	if err := deliverBounce(t, h.addr, "sender@external.example", "support@bnc.example", msg); err != nil {
		t.Fatalf("mailbox delivery: %v", err)
	}

	// An inbound message row + a pending webhook delivery exist.
	var direction, status string
	var n int
	if err := testStore.Pool.QueryRow(ctx,
		"SELECT direction, status FROM messages WHERE direction='inbound' ORDER BY created_at DESC LIMIT 1").Scan(&direction, &status); err != nil {
		t.Fatalf("no inbound message: %v", err)
	}
	if status != "received" {
		t.Errorf("status = %s, want received", status)
	}
	_ = testStore.Pool.QueryRow(ctx, "SELECT count(*) FROM webhook_deliveries WHERE result='pending'").Scan(&n)
	if n != 1 {
		t.Errorf("pending webhook deliveries = %d, want 1", n)
	}
}

func TestInboundRejectsUnknownMailbox(t *testing.T) {
	h := setup(t)
	defer h.cleanup()
	ctx := context.Background()
	d, _ := testStore.GetDomainByName(ctx, "bnc.example")
	_, _ = testStore.SetDomainReceiving(ctx, store.SetDomainReceivingParams{ID: d.ID, Receiving: true})

	// No mailbox for "ghost" and no catch-all → 550.
	err := deliverBounce(t, h.addr, "s@ext.example", "ghost@bnc.example", "From: s@ext.example\r\n\r\nhi\r\n")
	if err == nil {
		t.Fatal("unknown mailbox should be rejected")
	}
	if se, ok := err.(*smtp.SMTPError); ok && se.Code != 550 {
		t.Errorf("code = %d, want 550", se.Code)
	}
}
