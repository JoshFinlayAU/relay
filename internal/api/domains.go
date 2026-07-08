package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/dkim"
	"relay/internal/dns"
	"relay/internal/store"
)

// ---- DTOs -----------------------------------------------------------------

type domainDTO struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	Receiving       bool       `json:"receiving"`
	SendingPaused   bool       `json:"sending_paused"`
	ForwardBounces  bool       `json:"forward_bounces"`
	BounceSubdomain string     `json:"bounce_subdomain"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

func toDomainDTO(d store.Domain) domainDTO {
	return domainDTO{
		ID:              d.ID.String(),
		Name:            d.Name,
		Status:          d.Status,
		Receiving:       d.Receiving,
		SendingPaused:   d.SendingPaused,
		ForwardBounces:  d.ForwardBounces,
		BounceSubdomain: d.BounceSubdomain,
		CreatedAt:       tsPtr(d.CreatedAt),
		UpdatedAt:       tsPtr(d.UpdatedAt),
	}
}

// ---- Create ---------------------------------------------------------------

type createDomainReq struct {
	Name      string `json:"name"`
	Receiving bool   `json:"receiving"`
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var req createDomainReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	name := normalizeDomain(req.Name)
	if !validDomainName(name) {
		errBadRequest(w, "invalid_domain", "name is not a valid domain")
		return
	}
	if _, err := s.Store.GetDomainByName(r.Context(), name); err == nil {
		errConflict(w, "domain_exists", "domain already registered")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		errInternal(w, s.Log, "get domain by name", err)
		return
	}

	token, err := randToken()
	if err != nil {
		errInternal(w, s.Log, "gen token", err)
		return
	}
	selector := dkim.Selector(time.Now().UTC().Year(), "a")
	kp, err := dkim.Generate(selector)
	if err != nil {
		errInternal(w, s.Log, "dkim generate", err)
		return
	}
	encPriv, err := s.Sealer.Seal(kp.PrivatePEM)
	if err != nil {
		errInternal(w, s.Log, "seal dkim key", err)
		return
	}

	var created store.Domain
	err = s.Store.Tx(r.Context(), func(q *store.Queries) error {
		d, err := q.CreateDomain(r.Context(), store.CreateDomainParams{
			Name:            name,
			Receiving:       req.Receiving,
			VerifyToken:     token,
			BounceSubdomain: dns.BounceSubdomain(name),
		})
		if err != nil {
			return err
		}
		created = d
		if _, err := q.InsertDKIMKey(r.Context(), store.InsertDKIMKeyParams{
			DomainID:      d.ID,
			Selector:      kp.Selector,
			Algorithm:     kp.Algorithm,
			PrivateKeyEnc: encPriv,
			PublicKey:     kp.PublicB64,
		}); err != nil {
			return err
		}
		for _, rec := range dns.PlanRecords(d.Name, d.VerifyToken, kp.Selector, kp.PublicB64, d.Receiving, s.Params) {
			if err := q.UpsertDNSRecord(r.Context(), store.UpsertDNSRecordParams{
				DomainID:      d.ID,
				Purpose:       string(rec.Purpose),
				Type:          rec.Type,
				Name:          rec.Name,
				ExpectedValue: rec.Value,
			}); err != nil {
				return err
			}
		}
		return q.EmitEvent(r.Context(), d.ID, "domain.created", map[string]any{"name": name})
	})
	if err != nil {
		errInternal(w, s.Log, "create domain tx", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"domain": toDomainDTO(created),
		"dns":    s.staticInstructions(created, kp.Selector, kp.PublicB64),
	})
}

// ---- List / Get -----------------------------------------------------------

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	rows, err := s.Store.ListDomains(r.Context(), store.ListDomainsParams{
		Limit:  int32(limit + 1), // fetch one extra to compute next cursor
		Offset: int32(offset),
	})
	if err != nil {
		errInternal(w, s.Log, "list domains", err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(offset + limit)
	}
	out := make([]domainDTO, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDomainDTO(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": out, "next_cursor": next})
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": toDomainDTO(d)})
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteDomain(r.Context(), d.ID); err != nil {
		errInternal(w, s.Log, "delete domain", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Patch (receiving / pause) -------------------------------------------

type patchDomainReq struct {
	Receiving      *bool `json:"receiving"`
	SendingPaused  *bool `json:"sending_paused"`
	ForwardBounces *bool `json:"forward_bounces"`
}

func (s *Server) handlePatchDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	var req patchDomainReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	var err error
	if req.Receiving != nil {
		d, err = s.Store.SetDomainReceiving(r.Context(), store.SetDomainReceivingParams{ID: d.ID, Receiving: *req.Receiving})
		if err == nil {
			// Re-plan records so the inbound MX appears/disappears.
			err = s.replanRecords(r.Context(), d)
		}
	}
	if err == nil && req.SendingPaused != nil {
		d, err = s.Store.SetDomainSendingPaused(r.Context(), store.SetDomainSendingPausedParams{ID: d.ID, SendingPaused: *req.SendingPaused})
		if err == nil {
			ev := "domain.sending_resumed"
			if *req.SendingPaused {
				ev = "domain.sending_paused"
			}
			_ = s.Store.EmitEvent(r.Context(), d.ID, ev, nil)
		}
	}
	if err == nil && req.ForwardBounces != nil {
		d, err = s.Store.SetDomainForwardBounces(r.Context(), store.SetDomainForwardBouncesParams{ID: d.ID, ForwardBounces: *req.ForwardBounces})
	}
	if err != nil {
		errInternal(w, s.Log, "patch domain", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": toDomainDTO(d)})
}

// ---- helpers --------------------------------------------------------------

func (s *Server) replanRecords(ctx context.Context, d store.Domain) error {
	key, err := s.Store.GetActiveDKIMKey(ctx, d.ID)
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, func(q *store.Queries) error {
		if err := q.DeleteDNSRecords(ctx, d.ID); err != nil {
			return err
		}
		for _, rec := range dns.PlanRecords(d.Name, d.VerifyToken, key.Selector, key.PublicKey, d.Receiving, s.Params) {
			if err := q.UpsertDNSRecord(ctx, store.UpsertDNSRecordParams{
				DomainID:      d.ID,
				Purpose:       string(rec.Purpose),
				Type:          rec.Type,
				Name:          rec.Name,
				ExpectedValue: rec.Value,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) loadDomain(w http.ResponseWriter, r *http.Request) (store.Domain, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return store.Domain{}, false
	}
	d, err := s.Store.GetDomain(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, "domain not found")
		return store.Domain{}, false
	}
	if err != nil {
		errInternal(w, s.Log, "get domain", err)
		return store.Domain{}, false
	}
	return d, true
}

func normalizeDomain(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// validDomainName does a conservative FQDN check (labels, length), enough to
// reject injection/garbage; DNS verification is the real gate.
func validDomainName(name string) bool {
	if len(name) < 3 || len(name) > 253 || !strings.Contains(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			isAlnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
			if !isAlnum && c != '-' {
				return false
			}
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

func randToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	if c := r.URL.Query().Get("cursor"); c != "" {
		if b, err := base64.RawURLEncoding.DecodeString(c); err == nil {
			if n, err := strconv.Atoi(string(b)); err == nil && n >= 0 {
				offset = n
			}
		}
	}
	return limit, offset
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}
