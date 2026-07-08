package api

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"relay/internal/bounce"
	"relay/internal/dkim"
	"relay/internal/store"
)

// cleanAddress parses s as a single RFC 5322 addr-spec and returns the bare,
// lower-cased address. It rejects display names, multiple addresses, and any
// control characters (CR/LF) - closing the header-injection vector where a
// crafted recipient like "a@b\r\nBcc: evil@x" would smuggle headers into the
// DKIM-signed test message.
func cleanAddress(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\r\n\x00") {
		return "", false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" {
		return "", false
	}
	got := strings.ToLower(addr.Address)
	// ParseAddress normalises; reject if it differs from the raw input (any
	// display-name/comment smuggling) or still contains an unexpected control.
	if strings.ContainsAny(got, "\r\n\x00 ") || !strings.Contains(got, "@") {
		return "", false
	}
	return got, true
}

type testSendReq struct {
	To   string `json:"to"`
	From string `json:"from"` // optional; defaults to relay-test@<domain>
}

// handleTestSend assembles, DKIM-signs, stores and enqueues a test message for a
// domain, then returns its id so the caller can poll the delivery trace via
// GET /v1/messages/{id}.
func (s *Server) handleTestSend(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	if d.Status == "suspended" || d.SendingPaused {
		errBadRequest(w, "sending_disabled", "sending is paused or the domain is suspended")
		return
	}
	var req testSendReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	to, ok := cleanAddress(req.To)
	if !ok {
		errBadRequest(w, "invalid_recipient", "to must be a single valid email address")
		return
	}
	from := "relay-test@" + d.Name
	if strings.TrimSpace(req.From) != "" {
		from, ok = cleanAddress(req.From)
		if !ok {
			errBadRequest(w, "invalid_from", "from must be a single valid email address")
			return
		}
	}
	if domainOf(from) != d.Name {
		errBadRequest(w, "invalid_from", "from must be on "+d.Name)
		return
	}

	key, err := s.Store.GetActiveDKIMKey(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "get dkim key", err)
		return
	}
	privPEM, err := s.Sealer.Open(key.PrivateKeyEnc)
	if err != nil {
		errInternal(w, s.Log, "open dkim key", err)
		return
	}
	signer, err := dkim.NewSigner(d.Name, key.Selector, privPEM)
	if err != nil {
		errInternal(w, s.Log, "signer", err)
		return
	}

	msgID := uuid.New()
	now := time.Now().UTC()
	raw := fmt.Sprintf("Received: by %s (Relay test-send) id %s; %s\r\n"+
		"From: %s\r\nTo: %s\r\nSubject: Relay test message\r\nDate: %s\r\n"+
		"Message-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"+
		"This is a Relay test message sent from the admin console at %s.\r\n",
		s.Hostname, msgID, now.Format(time.RFC1123Z),
		from, to, now.Format(time.RFC1123Z), msgID, s.Hostname, now.Format(time.RFC3339))

	signed, err := signer.Sign([]byte(raw))
	if err != nil {
		errInternal(w, s.Log, "sign", err)
		return
	}
	bodyRef, err := s.Blobs.Put(signed)
	if err != nil {
		errInternal(w, s.Log, "store body", err)
		return
	}
	verp := bounce.VERPAddress(msgID, d.BounceSubdomain)
	sel := key.Selector
	domID := d.ID
	if _, err := s.Store.InsertMessage(r.Context(), store.InsertMessageParams{
		ID: msgID, Direction: "outbound", DomainID: &domID, MailFrom: &verp,
		HeaderFrom: &from, RcptTo: []string{to}, Subject: strPtr("Relay test message"),
		SizeBytes: int64(len(signed)), DkimSelector: &sel, BodyRef: &bodyRef,
		VerpToken: strPtr(msgID.String()), Status: "queued",
	}); err != nil {
		errInternal(w, s.Log, "insert message", err)
		return
	}
	if _, err := s.Store.EnqueueDeliveryJob(r.Context(), store.EnqueueDeliveryJobParams{MessageID: msgID, Rcpt: to}); err != nil {
		errInternal(w, s.Log, "enqueue", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), d.ID, "domain.test_send", map[string]any{"to": to, "message_id": msgID.String()})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"message_id": msgID.String(),
		"to":         to,
		"trace_url":  "/v1/messages/" + msgID.String(),
	})
}

// domainOf returns the lower-cased domain part of an email address.
func domainOf(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}
