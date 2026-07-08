package api

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/store"
)

func uuidStr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func tsFrom(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

type messageDTO struct {
	ID           string     `json:"id"`
	Direction    string     `json:"direction"`
	Status       string     `json:"status"`
	MailFrom     *string    `json:"mail_from"`
	HeaderFrom   *string    `json:"header_from"`
	RcptTo       []string   `json:"rcpt_to"`
	Subject      *string    `json:"subject"`
	SizeBytes    int64      `json:"size_bytes"`
	DKIMSelector *string    `json:"dkim_selector"`
	DomainID     *string    `json:"domain_id"`
	CredentialID *string    `json:"credential_id"`
	CreatedAt    *time.Time `json:"created_at"`
}

func toMessageDTO(m store.Message) messageDTO {
	return messageDTO{
		ID:           m.ID.String(),
		Direction:    m.Direction,
		Status:       m.Status,
		MailFrom:     m.MailFrom,
		HeaderFrom:   m.HeaderFrom,
		RcptTo:       m.RcptTo,
		Subject:      m.Subject,
		SizeBytes:    m.SizeBytes,
		DKIMSelector: m.DkimSelector,
		DomainID:     uuidStr(m.DomainID),
		CredentialID: uuidStr(m.CredentialID),
		CreatedAt:    tsPtr(m.CreatedAt),
	}
}

type attemptDTO struct {
	Rcpt        string     `json:"rcpt"`
	MXHost      *string    `json:"mx_host"`
	Result      string     `json:"result"`
	SMTPCode    *int32     `json:"smtp_code"`
	SMTPResp    *string    `json:"smtp_response"`
	TLSVersion  *string    `json:"tls_version"`
	TLSVerified *bool      `json:"tls_verified"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	params := store.ListMessagesParams{Limit: int32(limit + 1), Offset: int32(offset)}
	if v := q.Get("direction"); v != "" {
		params.Direction = &v
	}
	if v := q.Get("status"); v != "" {
		params.Status = &v
	}
	if v := q.Get("domain_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.DomainID = &id
		}
	}
	if v := q.Get("credential_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.CredentialID = &id
		}
	}
	if v := q.Get("rcpt"); v != "" {
		params.Rcpt = &v
	}
	if v := q.Get("after"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			params.After = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	if v := q.Get("before"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			params.Before = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	rows, err := s.Store.ListMessages(r.Context(), params)
	if err != nil {
		errInternal(w, s.Log, "list messages", err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(offset + limit)
	}
	out := make([]messageDTO, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMessageDTO(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "next_cursor": next})
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	m, err := s.Store.GetMessage(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, "message not found")
		return
	}
	if err != nil {
		errInternal(w, s.Log, "get message", err)
		return
	}
	attempts, err := s.Store.ListDeliveryAttempts(r.Context(), id)
	if err != nil {
		errInternal(w, s.Log, "list attempts", err)
		return
	}
	out := make([]attemptDTO, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, attemptDTO{
			Rcpt: a.Rcpt, MXHost: a.MxHost, Result: a.Result,
			SMTPCode: a.SmtpCode, SMTPResp: a.SmtpResponse,
			TLSVersion: a.TlsVersion, TLSVerified: a.TlsVerified,
			StartedAt: tsPtr(a.StartedAt), FinishedAt: tsPtr(a.FinishedAt),
		})
	}
	bounces, _ := s.Store.ListBounceEvents(r.Context(), &id)
	bOut := make([]map[string]any, 0, len(bounces))
	for _, b := range bounces {
		bOut = append(bOut, map[string]any{
			"rcpt": b.Rcpt, "type": b.Type, "dsn_code": b.DsnCode, "created_at": tsPtr(b.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": toMessageDTO(m), "attempts": out, "bounces": bOut})
}

// handleGetMessageRaw returns the raw RFC 5322 headers of the stored message
// (the block up to the first blank line). Bodies are content-addressed on disk
// and subject to retention, so this is best-effort: 404 once the body is gone.
func (s *Server) handleGetMessageRaw(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	m, err := s.Store.GetMessage(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, "message not found")
		return
	}
	if err != nil {
		errInternal(w, s.Log, "get message", err)
		return
	}
	if m.BodyRef == nil {
		errNotFound(w, "raw message no longer available (retention)")
		return
	}
	raw, err := s.Blobs.Get(*m.BodyRef)
	if err != nil {
		errNotFound(w, "raw message no longer available (retention)")
		return
	}
	headers := raw
	// Split on the first blank line (CRLFCRLF, else LFLF).
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		headers = raw[:i]
	} else if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		headers = raw[:i]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(headers)
}

// handleStatsOverview powers the dashboard.
func (s *Server) handleStatsOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	since := time.Now().Add(-24 * time.Hour)
	counts, _ := s.Store.MessageStatusCounts(ctx, tsFrom(since))
	byStatus := map[string]int64{}
	for _, c := range counts {
		byStatus[c.Status] = c.N
	}
	depth, _ := s.Store.QueueDepth(ctx)
	degraded, _ := s.Store.CountDegradedDomains(ctx)
	events, _ := s.Store.ListEvents(ctx, store.ListEventsParams{Limit: 10, Offset: 0})
	evOut := make([]map[string]any, 0, len(events))
	for _, e := range events {
		evOut = append(evOut, map[string]any{
			"type": e.Type, "detail": e.Detail, "created_at": tsPtr(e.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window":           "24h",
		"queue_depth":      depth,
		"degraded_domains": degraded,
		"by_status":        byStatus,
		"recent_events":    evOut,
	})
}
