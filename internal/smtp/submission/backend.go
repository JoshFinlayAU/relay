// Package submission implements the 587/465 SMTP submission backend: AUTH over
// TLS, credential-scope enforcement on MAIL FROM and header From, DKIM signing,
// VERP return paths, and enqueue of per-recipient delivery jobs.
package submission

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"

	"relay/internal/auth"
	"relay/internal/bounce"
	"relay/internal/creds"
	"relay/internal/crypto"
	"relay/internal/dkim"
	"relay/internal/storage"
	"relay/internal/store"
)

// Deps are the collaborators the submission backend needs.
type Deps struct {
	Store           *store.Store
	Auth            *auth.Authenticator
	Sealer          *crypto.Sealer
	Blobs           *storage.Store
	Log             *slog.Logger
	Hostname        string
	MaxMessageBytes int64
	Submitted       prometheus.Counter // incremented per accepted message
}

// Backend is a go-smtp Backend for submission.
type Backend struct{ deps Deps }

// New builds a submission Backend.
func New(deps Deps) *Backend { return &Backend{deps: deps} }

// NewSession starts a new SMTP session.
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	ip := ""
	if addr := c.Conn().RemoteAddr(); addr != nil {
		if host, _, err := net.SplitHostPort(addr.String()); err == nil {
			ip = host
		}
	}
	return &session{deps: b.deps, conn: c, remoteIP: ip}, nil
}

type session struct {
	deps     Deps
	conn     *smtp.Conn
	remoteIP string

	cred  *store.Credential
	restr creds.Restrictions
	from  string
	rcpts []string
}

func (s *session) AuthMechanisms() []string { return []string{"PLAIN", "LOGIN"} }

func (s *session) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case "PLAIN":
		return sasl.NewPlainServer(func(_, username, password string) error {
			return s.doAuth(username, password)
		}), nil
	case "LOGIN":
		return newLoginServer(s.doAuth), nil
	default:
		return nil, smtp.ErrAuthUnsupported
	}
}

func (s *session) doAuth(username, password string) error {
	cred, err := s.deps.Auth.Authenticate(context.Background(), username, password, s.remoteIP)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrRateLimited):
			return &smtp.SMTPError{Code: 454, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "too many attempts, try again later"}
		case errors.Is(err, auth.ErrLocked):
			return &smtp.SMTPError{Code: 454, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "credential temporarily locked"}
		default:
			return &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 8}, Message: "authentication failed"}
		}
	}
	s.cred = cred
	s.restr, _ = creds.Parse(cred.Restrictions)
	return nil
}

func (s *session) Mail(from string, opts *smtp.MailOptions) error {
	if s.cred == nil {
		return &smtp.SMTPError{Code: 530, EnhancedCode: smtp.EnhancedCode{5, 7, 0}, Message: "authentication required"}
	}
	from = strings.ToLower(strings.TrimSpace(from))
	if from == "" {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "null sender not allowed on submission"}
	}
	if opts != nil && s.deps.MaxMessageBytes > 0 && opts.Size > s.deps.MaxMessageBytes {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message too large"}
	}
	if s.restr.MaxMessageSize > 0 && opts != nil && opts.Size > s.restr.MaxMessageSize {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message exceeds credential size limit"}
	}
	// Envelope MAIL FROM domain must be covered by the credential.
	if _, err := s.coveredDomain(domainOf(from)); err != nil {
		return err
	}
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	if s.cred == nil {
		return &smtp.SMTPError{Code: 530, EnhancedCode: smtp.EnhancedCode{5, 7, 0}, Message: "authentication required"}
	}
	max := s.restr.MaxRecipients
	if max > 0 && len(s.rcpts) >= max {
		return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 5, 3}, Message: "too many recipients"}
	}
	s.rcpts = append(s.rcpts, strings.ToLower(strings.TrimSpace(to)))
	return nil
}

func (s *session) Data(r io.Reader) error {
	if s.cred == nil || s.from == "" || len(s.rcpts) == 0 {
		return &smtp.SMTPError{Code: 503, EnhancedCode: smtp.EnhancedCode{5, 5, 1}, Message: "bad sequence of commands"}
	}
	ctx := context.Background()

	limit := s.deps.MaxMessageBytes
	if limit <= 0 {
		limit = 50 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "error reading message"}
	}
	if int64(len(raw)) > limit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message too large"}
	}

	// Header From must be present and within scope.
	hdrFrom, err := headerFrom(raw)
	if err != nil {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: err.Error()}
	}
	if !s.restr.FromAllowed(hdrFrom) {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "From address not permitted for this credential"}
	}
	dom, err := s.coveredDomain(domainOf(hdrFrom))
	if err != nil {
		return err
	}

	// Suppression check: reject hard-bounced/complained recipients unless the
	// credential is explicitly allowed to override.
	if !s.restr.SuppressionOverride {
		for _, rcpt := range s.rcpts {
			suppressed, serr := s.deps.Store.IsSuppressed(ctx, store.IsSuppressedParams{DomainID: dom.ID, Address: rcpt})
			if serr == nil && suppressed {
				return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "recipient " + rcpt + " is suppressed (previously hard-bounced or complained)"}
			}
		}
	}

	// Per-credential hourly rate limit.
	if s.restr.MaxMessagesPerHour > 0 {
		cutoff := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
		n, err := s.deps.Store.CountRecentMessagesByCredential(ctx, store.CountRecentMessagesByCredentialParams{
			CredentialID: &s.cred.ID, CreatedAt: cutoff,
		})
		if err == nil && int(n)+1 > s.restr.MaxMessagesPerHour {
			return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "hourly rate limit exceeded"}
		}
	}

	// DKIM signer for the sending domain.
	key, err := s.deps.Store.GetActiveDKIMKey(ctx, dom.ID)
	if err != nil {
		return s.temp("no signing key for domain")
	}
	privPEM, err := s.deps.Sealer.Open(key.PrivateKeyEnc)
	if err != nil {
		return s.temp("cannot access signing key")
	}
	signer, err := dkim.NewSigner(dom.Name, key.Selector, privPEM)
	if err != nil {
		return s.temp("signer init failed")
	}

	msgID := uuid.New()
	assembled := assemble(raw, assembleOptions{
		receivedHeader: s.receivedHeader(msgID),
		messageID:      fmt.Sprintf("<%s@%s>", msgID, s.deps.Hostname),
		date:           time.Now(),
	})
	signed, err := signer.Sign(assembled)
	if err != nil {
		s.deps.Log.Error("dkim sign", "err", err, "domain", dom.Name)
		return s.temp("signing failed")
	}

	bodyRef, err := s.deps.Blobs.Put(signed)
	if err != nil {
		return s.temp("storage failed")
	}

	verp := bounce.VERPAddress(msgID, dom.BounceSubdomain)
	subject := subjectOf(raw)
	credID := s.cred.ID
	domID := dom.ID
	sel := key.Selector
	verpTok := msgID.String()

	if _, err := s.deps.Store.InsertMessage(ctx, store.InsertMessageParams{
		ID:           msgID,
		Direction:    "outbound",
		CredentialID: &credID,
		DomainID:     &domID,
		MailFrom:     &verp,
		HeaderFrom:   &hdrFrom,
		RcptTo:       s.rcpts,
		Subject:      &subject,
		SizeBytes:    int64(len(signed)),
		DkimSelector: &sel,
		BodyRef:      &bodyRef,
		VerpToken:    &verpTok,
		Status:       "queued",
	}); err != nil {
		s.deps.Log.Error("insert message", "err", err)
		return s.temp("could not queue message")
	}
	for _, rcpt := range s.rcpts {
		if _, err := s.deps.Store.EnqueueDeliveryJob(ctx, store.EnqueueDeliveryJobParams{MessageID: msgID, Rcpt: rcpt}); err != nil {
			s.deps.Log.Error("enqueue job", "err", err, "rcpt", rcpt)
		}
	}
	_ = s.deps.Store.TouchCredentialLastUsed(ctx, s.cred.ID)
	if s.deps.Submitted != nil {
		s.deps.Submitted.Inc()
	}
	s.deps.Log.Info("message accepted", "msg_id", msgID, "domain", dom.Name, "rcpts", len(s.rcpts), "size", len(signed), "client_ip", s.remoteIP)

	// Reset envelope for a possible next message on the same connection.
	s.from = ""
	s.rcpts = nil
	return nil
}

// coveredDomain returns the domain if the authenticated credential may send for
// it (covered scope, active/degraded, not paused/suspended).
func (s *session) coveredDomain(name string) (*store.Domain, error) {
	deny := &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "sender domain not permitted for this credential"}
	if name == "" {
		return nil, deny
	}
	dom, err := s.deps.Store.GetDomainByName(context.Background(), name)
	if err != nil {
		return nil, deny
	}
	covers, err := s.deps.Store.CredentialCoversDomain(context.Background(), store.CredentialCoversDomainParams{ID: s.cred.ID, DomainID: dom.ID})
	if err != nil || covers == nil || !*covers {
		return nil, deny
	}
	if dom.Status == "suspended" || dom.SendingPaused {
		return nil, &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "sending is paused for this domain"}
	}
	return &dom, nil
}

// receivedHeader builds our Received header. It deliberately does NOT include
// the submitting app's IP (CLAUDE.md: never leak internal app IPs); the client
// is identified by its authenticated EHLO name. A single recipient is recorded
// in a "for" clause per RFC 5321.
func (s *session) receivedHeader(msgID uuid.UUID) string {
	ehlo := sanitizeHeader(s.conn.Hostname())
	if ehlo == "" {
		ehlo = "authenticated-client"
	}
	forClause := ""
	if len(s.rcpts) == 1 {
		forClause = " for <" + sanitizeHeader(s.rcpts[0]) + ">"
	}
	return fmt.Sprintf("from %s (authenticated) by %s (Relay) with ESMTPA id %s%s; %s",
		ehlo, s.deps.Hostname, msgID, forClause, time.Now().UTC().Format(time.RFC1123Z))
}

// sanitizeHeader strips CR, LF, and other control characters to prevent header
// injection via client-supplied values (EHLO name, recipient) in our Received.
func sanitizeHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func (s *session) temp(msg string) error {
	return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: msg}
}

func (s *session) Reset()        { s.from = ""; s.rcpts = nil }
func (s *session) Logout() error { return nil }
