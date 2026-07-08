package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relay/internal/auth"
	"relay/internal/store"
)

// validWebhookURL requires an absolute http(s) URL with a host and rejects
// obvious SSRF targets that literal-IP forms could reach: loopback and
// link-local (169.254.0.0/16, incl. cloud metadata). Private LAN targets are
// permitted - Relay's webhook consumers are internal apps. (DNS-rebind is out
// of scope; targets are admin-configured.)
func validWebhookURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return false
		}
	}
	return true
}

type mailboxDTO struct {
	ID         string     `json:"id"`
	DomainID   string     `json:"domain_id"`
	LocalPart  string     `json:"local_part"`
	WebhookURL string     `json:"webhook_url"`
	Status     string     `json:"status"`
	CreatedAt  *time.Time `json:"created_at"`
}

func toMailboxDTO(m store.Mailbox) mailboxDTO {
	return mailboxDTO{
		ID: m.ID.String(), DomainID: m.DomainID.String(), LocalPart: m.LocalPart,
		WebhookURL: m.WebhookUrl, Status: m.Status, CreatedAt: tsPtr(m.CreatedAt),
	}
}

type createMailboxReq struct {
	LocalPart  string `json:"local_part"`
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	var req createMailboxReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	lp := strings.ToLower(strings.TrimSpace(req.LocalPart))
	if lp != "*" && !validLocalPart(lp) {
		errBadRequest(w, "invalid_local_part", "local_part must be a mailbox name or '*' for catch-all")
		return
	}
	if !validWebhookURL(req.WebhookURL) {
		errBadRequest(w, "invalid_webhook_url", "webhook_url must be an http(s) URL and not a loopback/link-local address")
		return
	}
	secret := strings.TrimSpace(req.Secret)
	if secret == "" {
		var err error
		if secret, err = auth.GenerateSecret(); err != nil {
			errInternal(w, s.Log, "gen webhook secret", err)
			return
		}
	}
	enc, err := s.Sealer.Seal([]byte(secret))
	if err != nil {
		errInternal(w, s.Log, "seal webhook secret", err)
		return
	}
	mb, err := s.Store.CreateMailbox(r.Context(), store.CreateMailboxParams{
		DomainID: d.ID, LocalPart: lp, WebhookUrl: req.WebhookURL, WebhookSecretEnc: enc,
	})
	if err != nil {
		errConflict(w, "mailbox_exists", "a mailbox with this local part already exists")
		return
	}
	_ = s.Store.EmitEvent(r.Context(), d.ID, "mailbox.created", map[string]any{"local_part": lp})
	// Return the signing secret once (like credentials).
	writeJSON(w, http.StatusCreated, map[string]any{"mailbox": toMailboxDTO(mb), "secret": secret})
}

func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.ListMailboxesByDomain(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "list mailboxes", err)
		return
	}
	out := make([]mailboxDTO, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMailboxDTO(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": out})
}

func (s *Server) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	if _, err := s.Store.GetMailbox(r.Context(), id); errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, "mailbox not found")
		return
	}
	if err := s.Store.DeleteMailbox(r.Context(), id); err != nil {
		errInternal(w, s.Log, "delete mailbox", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- webhook delivery log ---

type webhookDeliveryDTO struct {
	ID           string     `json:"id"`
	MessageID    string     `json:"message_id"`
	AttemptNo    int32      `json:"attempt_no"`
	StatusCode   *int32     `json:"status_code"`
	Result       string     `json:"result"`
	ResponseSnip *string    `json:"response_snippet"`
	CreatedAt    *time.Time `json:"created_at"`
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	limit, offset := parsePagination(r)
	rows, err := s.Store.ListWebhookDeliveriesByDomain(r.Context(), store.ListWebhookDeliveriesByDomainParams{
		DomainID: d.ID, Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		errInternal(w, s.Log, "list webhook deliveries", err)
		return
	}
	out := make([]webhookDeliveryDTO, 0, len(rows))
	for _, wd := range rows {
		out = append(out, webhookDeliveryDTO{
			ID: wd.ID.String(), MessageID: wd.MessageID.String(), AttemptNo: wd.AttemptNo,
			StatusCode: wd.StatusCode, Result: wd.Result, ResponseSnip: wd.ResponseSnippet,
			CreatedAt: tsPtr(wd.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": out})
}

func (s *Server) handleRedeliverWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	if _, err := s.Store.GetWebhookDelivery(r.Context(), id); errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, "delivery not found")
		return
	}
	if err := s.Store.RequeueWebhookDelivery(r.Context(), id); err != nil {
		errInternal(w, s.Log, "requeue webhook", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
