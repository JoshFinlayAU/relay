// Package inbound implements the port-25 listener. In Phase 5 it accepts ONLY
// VERP bounce-subdomain recipients (async bounces routed back to us); every
// other recipient is rejected 550 5.7.1 so there is no open relay. Mailbox
// receiving + webhooks arrive in Phase 6.
package inbound

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"strings"

	"blitiri.com.ar/go/spf"
	"github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relay/internal/bounce"
	"relay/internal/dkim"
	"relay/internal/dmarc"
	"relay/internal/storage"
	"relay/internal/store"
)

// Deps are the inbound backend's collaborators.
type Deps struct {
	Store           *store.Store
	Blobs           *storage.Store
	Log             *slog.Logger
	Hostname        string
	MaxMessageBytes int64
	// Submission is the 587/465 backend; connections from AuthSubnets on port 25
	// are delegated to it so trusted networks can submit AUTH'd mail (CLAUDE.md).
	Submission  smtp.Backend
	AuthSubnets []*net.IPNet
	// DMARC ingests aggregate reports sent to dmarc@<hostname>.
	DMARC *dmarc.Ingester
}

// Backend is a go-smtp Backend for inbound mail (port 25).
type Backend struct{ deps Deps }

func New(deps Deps) *Backend { return &Backend{deps: deps} }

func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	ip := ""
	if a := c.Conn().RemoteAddr(); a != nil {
		if h, _, err := net.SplitHostPort(a.String()); err == nil {
			ip = h
		}
	}
	// Trusted subnets on :25 get the full submission flow (AUTH + scoped send).
	if b.deps.Submission != nil && ipInAny(ip, b.deps.AuthSubnets) {
		return b.deps.Submission.NewSession(c)
	}
	return &session{deps: b.deps, remoteIP: ip}, nil
}

func ipInAny(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n != nil && n.Contains(parsed) {
			return true
		}
	}
	return false
}

type session struct {
	deps     Deps
	remoteIP string

	mailFrom  string
	bounceFor *store.Message // original message a bounce recipient maps to
	mailbox   *store.Mailbox // matched inbound mailbox (non-bounce)
	inRcpt    string         // recipient address for a mailbox delivery
	isDMARC   bool           // recipient is dmarc@<hostname> (aggregate report)
}

// dmarcAddr is the mailbox that receives aggregate reports.
func (s *session) dmarcAddr() string { return "dmarc@" + strings.ToLower(s.deps.Hostname) }

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	// Bounces frequently use the null sender <>; accept anything here - the RCPT
	// check is what gates acceptance.
	s.mailFrom = strings.ToLower(strings.TrimSpace(from))
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	reject := &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "relay access denied"}
	to = strings.ToLower(strings.TrimSpace(to))
	ctx := context.Background()

	// (a) VERP bounce-subdomain recipient.
	if msgID, ok := bounce.DecodeVERP(to); ok {
		dom := bounceDomainOf(to)
		if dom == "" {
			return reject
		}
		msg, err := s.deps.Store.GetMessage(ctx, msgID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "unknown bounce reference"}
		}
		if err != nil {
			return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "temporary error"}
		}
		if msg.DomainID == nil {
			return reject
		}
		d, err := s.deps.Store.GetDomain(ctx, *msg.DomainID)
		if err != nil || !strings.EqualFold(d.BounceSubdomain, dom) {
			return reject
		}
		s.bounceFor = &msg
		return nil
	}

	// (b) DMARC aggregate reports to dmarc@<hostname>.
	if s.deps.DMARC != nil && to == s.dmarcAddr() {
		s.isDMARC = true
		return nil
	}

	// (c) Mailbox recipient on a receiving-enabled domain (exact or catch-all).
	local, domName := splitAddr(to)
	if domName == "" {
		return reject
	}
	d, err := s.deps.Store.GetDomainByName(ctx, domName)
	if err != nil || !d.Receiving {
		return reject
	}
	mb, err := s.deps.Store.FindMailbox(ctx, store.FindMailboxParams{DomainID: d.ID, LocalPart: local})
	if err != nil {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "no such mailbox"}
	}
	s.mailbox = &mb
	s.inRcpt = to
	return nil
}

func (s *session) Data(r io.Reader) error {
	if s.bounceFor == nil && s.mailbox == nil && !s.isDMARC {
		return &smtp.SMTPError{Code: 503, EnhancedCode: smtp.EnhancedCode{5, 5, 1}, Message: "bad sequence"}
	}
	ctx := context.Background()
	limit := s.deps.MaxMessageBytes
	if limit <= 0 {
		limit = 26 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message too large"}
	}

	if s.isDMARC {
		if n, err := s.deps.DMARC.Ingest(ctx, raw); err != nil {
			s.deps.Log.Warn("dmarc ingest", "err", err)
		} else {
			s.deps.Log.Info("dmarc reports ingested", "count", n)
		}
		return nil // always accept the report so senders don't retry
	}

	if s.mailbox != nil {
		return s.handleMailboxMail(ctx, raw)
	}

	res := bounce.Parse(raw)
	rawRef, _ := s.deps.Blobs.Put(raw) // best-effort archive of the bounce

	msg := s.bounceFor
	rcpt := res.Recipient
	if rcpt == "" && len(msg.RcptTo) == 1 {
		rcpt = msg.RcptTo[0]
	}

	bType := string(res.Type)
	if res.Type == bounce.TypeUnknown {
		bType = "soft" // conservative: don't suppress on an unparseable bounce
	}

	var rawRefPtr, dsn, rcptPtr *string
	if rawRef != "" {
		rawRefPtr = &rawRef
	}
	if res.Status != "" {
		dsn = &res.Status
	}
	if rcpt != "" {
		rcptPtr = &rcpt
	}
	if _, err := s.deps.Store.InsertBounceEvent(ctx, store.InsertBounceEventParams{
		MessageID: &msg.ID, Rcpt: rcptPtr, Type: bType, DsnCode: dsn, RawRef: rawRefPtr,
	}); err != nil {
		s.deps.Log.Error("insert bounce event", "err", err, "msg", msg.ID)
	}

	// Hard bounce / complaint → fail the recipient and suppress the address.
	if (res.Type == bounce.TypeHard || res.Type == bounce.TypeComplaint) && rcpt != "" && msg.DomainID != nil {
		reason := "hard bounce " + res.Status
		if res.Type == bounce.TypeComplaint {
			reason = "complaint"
		}
		_ = s.deps.Store.FailJobByRcpt(ctx, store.FailJobByRcptParams{
			MessageID: msg.ID, Rcpt: rcpt, LastCode: dsnCodeInt(res.Status), LastResponse: &reason,
		})
		if _, err := s.deps.Store.AddSuppression(ctx, store.AddSuppressionParams{
			DomainID: *msg.DomainID, Address: rcpt, Reason: &reason,
		}); err != nil {
			s.deps.Log.Error("add suppression", "err", err)
		}
		_ = s.deps.Store.EmitEvent(ctx, *msg.DomainID, "bounce.suppressed", map[string]any{
			"message_id": msg.ID.String(), "rcpt": rcpt, "type": bType, "status": res.Status,
		})
		s.updateMessageStatus(ctx, msg.ID)
	}

	// Optional: forward the bounce to a webhook if the domain opts in and has a
	// "bounces" (or catch-all) mailbox. The raw DSN is stored as an inbound
	// message and dispatched like any other webhook.
	if msg.DomainID != nil && rawRef != "" {
		if d, err := s.deps.Store.GetDomain(ctx, *msg.DomainID); err == nil && d.ForwardBounces {
			if mb, err := s.deps.Store.FindMailbox(ctx, store.FindMailboxParams{DomainID: d.ID, LocalPart: "bounces"}); err == nil {
				subject := "Bounce: " + bType + " " + res.Status
				im, ierr := s.deps.Store.InsertMessage(ctx, store.InsertMessageParams{
					ID: uuid.New(), Direction: "inbound", DomainID: &d.ID,
					MailFrom: strPtrOrNil(s.mailFrom), RcptTo: []string{rcpt}, Subject: &subject,
					SizeBytes: 0, BodyRef: &rawRef, Status: "received",
				})
				if ierr == nil {
					_, _ = s.deps.Store.CreateWebhookDelivery(ctx, store.CreateWebhookDeliveryParams{MailboxID: mb.ID, MessageID: im.ID})
				}
			}
		}
	}

	s.deps.Log.Info("bounce processed", "msg", msg.ID, "type", bType, "status", res.Status, "rcpt", rcpt)
	s.bounceFor = nil
	return nil
}

func (s *session) updateMessageStatus(ctx context.Context, id uuid.UUID) {
	counts, err := s.deps.Store.JobStatusCounts(ctx, id)
	if err != nil {
		return
	}
	if counts.Pending > 0 {
		return
	}
	status := "bounced"
	if counts.Delivered > 0 && counts.Failed > 0 {
		status = "partial"
	}
	_ = s.deps.Store.SetMessageStatus(ctx, store.SetMessageStatusParams{ID: id, Status: status})
}

// handleMailboxMail stores an inbound message, records SPF/DKIM results, and
// enqueues a webhook delivery. SPF/DKIM are informational (not blocking) in v1.
func (s *session) handleMailboxMail(ctx context.Context, raw []byte) error {
	bodyRef, err := s.deps.Blobs.Put(raw)
	if err != nil {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "storage error"}
	}
	spfRes := s.checkSPF(ctx)
	dkimRes := checkDKIM(raw)
	subject := subjectOf(raw)
	from := headerFromOf(raw)

	domID := s.mailbox.DomainID
	msg, err := s.deps.Store.InsertMessage(ctx, store.InsertMessageParams{
		ID: uuid.New(), Direction: "inbound", DomainID: &domID,
		MailFrom: strPtrOrNil(s.mailFrom), HeaderFrom: strPtrOrNil(from),
		RcptTo: []string{s.inRcpt}, Subject: strPtrOrNil(subject), SizeBytes: int64(len(raw)),
		BodyRef: &bodyRef, SpfResult: strPtrOrNil(spfRes), DkimResult: strPtrOrNil(dkimRes),
		Status: "received",
	})
	if err != nil {
		s.deps.Log.Error("insert inbound message", "err", err)
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "could not accept message"}
	}
	if _, err := s.deps.Store.CreateWebhookDelivery(ctx, store.CreateWebhookDeliveryParams{
		MailboxID: s.mailbox.ID, MessageID: msg.ID,
	}); err != nil {
		s.deps.Log.Error("create webhook delivery", "err", err)
	}
	s.deps.Log.Info("inbound message accepted", "mailbox", s.mailbox.LocalPart, "msg", msg.ID, "spf", spfRes, "dkim", dkimRes)
	s.mailbox = nil
	s.inRcpt = ""
	return nil
}

// checkSPF evaluates SPF for the connecting IP against the MAIL FROM domain.
func (s *session) checkSPF(ctx context.Context) string {
	ip := net.ParseIP(s.remoteIP)
	if ip == nil || s.mailFrom == "" {
		return "none"
	}
	res, _ := spf.CheckHostWithSender(ip, "", s.mailFrom)
	return string(res)
}

// checkDKIM verifies DKIM signatures on the raw message (real DNS lookup).
func checkDKIM(raw []byte) string {
	vs, err := dkim.Verify(raw)
	if err != nil || len(vs) == 0 {
		return "none"
	}
	for _, v := range vs {
		if v.Err == nil {
			return "pass"
		}
	}
	return "fail"
}

func (s *session) Reset()        { s.mailFrom = ""; s.bounceFor = nil; s.mailbox = nil; s.inRcpt = "" }
func (s *session) Logout() error { return nil }

func splitAddr(addr string) (local, domain string) {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return "", ""
	}
	return strings.ToLower(addr[:at]), strings.ToLower(addr[at+1:])
}

func subjectOf(raw []byte) string {
	if m, err := mail.ReadMessage(bytes.NewReader(raw)); err == nil {
		return m.Header.Get("Subject")
	}
	return ""
}

func headerFromOf(raw []byte) string {
	if m, err := mail.ReadMessage(bytes.NewReader(raw)); err == nil {
		if a, err := m.Header.AddressList("From"); err == nil && len(a) > 0 {
			return a[0].Address
		}
	}
	return ""
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// bounceDomainOf extracts the hosted domain from a bounce-subdomain address,
// e.g. bounce-<id>@bounce.example.com → bounce.example.com.
func bounceDomainOf(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

func dsnCodeInt(status string) *int32 {
	// Map an RFC 3463 status like 5.1.1 to an SMTP-ish code for storage.
	if strings.HasPrefix(status, "5.") {
		v := int32(550)
		return &v
	}
	if strings.HasPrefix(status, "4.") {
		v := int32(450)
		return &v
	}
	return nil
}
