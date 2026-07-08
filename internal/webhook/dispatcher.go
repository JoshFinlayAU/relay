package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"

	"relay/internal/crypto"
	"relay/internal/storage"
	"relay/internal/store"
)

// defaultRetrySchedule is the webhook backoff (CLAUDE.md): 1m, 5m, 30m, 2h, 6h.
var defaultRetrySchedule = []time.Duration{
	1 * time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour,
}

// MaxAge is the default dead-letter age (kept for reference/tests).
const MaxAge = 24 * time.Hour

// Metrics are optional (nil-safe).
type Metrics struct {
	Delivered  prometheus.Counter
	DeadLetter prometheus.Counter
}

// Dispatcher delivers inbound-message webhooks with retries + dead-lettering.
type Dispatcher struct {
	Store   *store.Store
	Blobs   *storage.Store
	Sealer  *crypto.Sealer
	Log     *slog.Logger
	Client  *http.Client
	Metrics Metrics

	// Config-driven retry backoff (defaults to defaultRetrySchedule / MaxAge).
	Schedule []time.Duration
	MaxAge   time.Duration

	poll time.Duration
}

// Run polls for pending webhook deliveries until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	if d.Client == nil {
		d.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if d.poll <= 0 {
		d.poll = 2 * time.Second
	}
	if len(d.Schedule) == 0 {
		d.Schedule = defaultRetrySchedule
	}
	if d.MaxAge <= 0 {
		d.MaxAge = MaxAge
	}
	t := time.NewTicker(d.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.tick(ctx)
		}
	}
}

func (d *Dispatcher) tick(ctx context.Context) {
	jobs, err := d.Store.ClaimWebhookDeliveries(ctx, 16)
	if err != nil {
		return
	}
	for _, j := range jobs {
		d.deliver(ctx, j)
	}
}

func (d *Dispatcher) deliver(ctx context.Context, wd store.WebhookDelivery) {
	mb, err := d.Store.GetMailbox(ctx, wd.MailboxID)
	if err != nil {
		d.fail(ctx, wd, 0, "mailbox missing")
		return
	}
	msg, err := d.Store.GetMessage(ctx, wd.MessageID)
	if err != nil || msg.BodyRef == nil {
		d.fail(ctx, wd, 0, "message missing")
		return
	}
	raw, err := d.Blobs.Get(*msg.BodyRef)
	if err != nil {
		d.retryOrDead(ctx, wd, 0, "body unreadable")
		return
	}

	payload := BuildPayload(raw, d.Blobs)
	payload.MessageID = msg.ID.String()
	if msg.SpfResult != nil {
		payload.SPFResult = *msg.SpfResult
	}
	if msg.DkimResult != nil {
		payload.DKIMResult = *msg.DkimResult
	}
	body, _ := json.Marshal(payload)

	secret, err := d.Sealer.Open(mb.WebhookSecretEnc)
	if err != nil {
		d.retryOrDead(ctx, wd, 0, "cannot access webhook secret")
		return
	}
	ts := strconv.FormatInt(nowUnix(), 10)
	sig := signBody(secret, ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mb.WebhookUrl, bytes.NewReader(body))
	if err != nil {
		d.fail(ctx, wd, 0, "bad webhook url")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Timestamp", ts)
	req.Header.Set("X-Relay-Signature", "sha256="+sig)
	req.Header.Set("User-Agent", "Relay-Webhook/1")

	resp, err := d.Client.Do(req)
	if err != nil {
		d.retryOrDead(ctx, wd, 0, "post failed: "+err.Error())
		return
	}
	snippet := readSnippet(resp.Body)
	resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = d.Store.MarkWebhookSuccess(ctx, store.MarkWebhookSuccessParams{
			ID: wd.ID, StatusCode: i32(resp.StatusCode), ResponseSnippet: strp(snippet),
		})
		if d.Metrics.Delivered != nil {
			d.Metrics.Delivered.Inc()
		}
		d.Log.Info("webhook delivered", "mailbox", mb.LocalPart, "code", resp.StatusCode, "msg", msg.ID)
		return
	}
	d.retryOrDead(ctx, wd, resp.StatusCode, snippet)
}

// retryOrDead schedules the next attempt or dead-letters after MaxAge / schedule exhaustion.
func (d *Dispatcher) retryOrDead(ctx context.Context, wd store.WebhookDelivery, code int, snippet string) {
	age := time.Duration(0)
	if wd.CreatedAt.Valid {
		age = nowT().Sub(wd.CreatedAt.Time)
	}
	sched := d.Schedule
	if len(sched) == 0 {
		sched = defaultRetrySchedule
	}
	maxAge := d.MaxAge
	if maxAge <= 0 {
		maxAge = MaxAge
	}
	attempt := int(wd.AttemptNo) // already incremented on claim
	if age >= maxAge || attempt > len(sched) {
		d.fail(ctx, wd, code, snippet)
		return
	}
	delay := sched[attempt-1]
	if attempt-1 >= len(sched) {
		delay = sched[len(sched)-1]
	}
	next := pgtype.Timestamptz{Time: nowT().Add(delay), Valid: true}
	_ = d.Store.MarkWebhookRetry(ctx, store.MarkWebhookRetryParams{
		ID: wd.ID, StatusCode: i32(code), ResponseSnippet: strp(snippet), NextAttemptAt: next,
	})
	d.Log.Info("webhook retry scheduled", "id", wd.ID, "attempt", attempt, "in", delay, "code", code)
}

func (d *Dispatcher) fail(ctx context.Context, wd store.WebhookDelivery, code int, snippet string) {
	_ = d.Store.MarkWebhookDeadLetter(ctx, store.MarkWebhookDeadLetterParams{
		ID: wd.ID, StatusCode: i32(code), ResponseSnippet: strp(snippet),
	})
	if d.Metrics.DeadLetter != nil {
		d.Metrics.DeadLetter.Inc()
	}
	// Emit a dead-letter event against the mailbox's domain.
	if mb, err := d.Store.GetMailbox(ctx, wd.MailboxID); err == nil {
		_ = d.Store.EmitEvent(ctx, mb.DomainID, "webhook.dead_letter", map[string]any{
			"mailbox": mb.LocalPart, "message_id": wd.MessageID.String(), "code": code,
		})
	}
	d.Log.Warn("webhook dead-lettered", "id", wd.ID, "code", code)
}

// signBody computes the HMAC-SHA256 over "<timestamp>.<body>".
func signBody(secret []byte, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func readSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	return string(b)
}

func i32(v int) *int32 {
	if v == 0 {
		return nil
	}
	n := int32(v)
	return &n
}
func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nowT / nowUnix are overridable in tests.
var nowT = time.Now

func nowUnix() int64 { return nowT().Unix() }

// Verify is a helper mirroring signBody so servers/tests can validate a webhook.
func Verify(secret []byte, ts string, body []byte, header string) bool {
	want := "sha256=" + signBody(secret, ts, body)
	return hmac.Equal([]byte(want), []byte(header))
}
