package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relay/internal/auth"
	"relay/internal/creds"
	"relay/internal/store"
)

type credentialDTO struct {
	ID           string             `json:"id"`
	DomainID     string             `json:"domain_id"`
	Username     string             `json:"username"`
	Status       string             `json:"status"`
	Restrictions creds.Restrictions `json:"restrictions"`
	LastUsed     *time.Time         `json:"last_used"`
	CreatedAt    *time.Time         `json:"created_at"`
	UpdatedAt    *time.Time         `json:"updated_at"`
}

func toCredentialDTO(c store.Credential) credentialDTO {
	r, _ := creds.Parse(c.Restrictions)
	return credentialDTO{
		ID:           c.ID.String(),
		DomainID:     c.DomainID.String(),
		Username:     c.Username,
		Status:       c.Status,
		Restrictions: r,
		LastUsed:     tsPtr(c.LastUsed),
		CreatedAt:    tsPtr(c.CreatedAt),
		UpdatedAt:    tsPtr(c.UpdatedAt),
	}
}

type createCredentialReq struct {
	Name         string              `json:"name"`
	Restrictions *creds.Restrictions `json:"restrictions"`
}

func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	var req createCredentialReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	local := strings.ToLower(strings.TrimSpace(req.Name))
	if !validLocalPart(local) {
		errBadRequest(w, "invalid_name", "name must be a valid mailbox local part")
		return
	}
	username := local + "@" + d.Name

	if _, err := s.Store.GetCredentialByUsername(r.Context(), username); err == nil {
		errConflict(w, "credential_exists", "a credential with this username already exists")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		errInternal(w, s.Log, "get credential by username", err)
		return
	}

	restr := creds.Restrictions{}
	if req.Restrictions != nil {
		restr = *req.Restrictions
	}
	if err := restr.Validate(); err != nil {
		errBadRequest(w, "invalid_restrictions", err.Error())
		return
	}
	restrJSON, _ := restr.JSON()

	secret, err := auth.GenerateSecret()
	if err != nil {
		errInternal(w, s.Log, "gen secret", err)
		return
	}
	hash, err := auth.HashSecret(secret)
	if err != nil {
		errInternal(w, s.Log, "hash secret", err)
		return
	}

	c, err := s.Store.CreateCredential(r.Context(), store.CreateCredentialParams{
		DomainID:     d.ID,
		Username:     username,
		SecretHash:   hash,
		Restrictions: restrJSON,
	})
	if err != nil {
		errInternal(w, s.Log, "create credential", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), d.ID, "credential.created", map[string]any{
		"credential_id": c.ID.String(), "username": username,
	})

	// Secret is returned exactly once.
	writeJSON(w, http.StatusCreated, map[string]any{
		"credential": toCredentialDTO(c),
		"secret":     secret,
	})
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	limit, offset := parsePagination(r)
	rows, err := s.Store.ListCredentialsByDomain(r.Context(), store.ListCredentialsByDomainParams{
		DomainID: d.ID, Limit: int32(limit + 1), Offset: int32(offset),
	})
	if err != nil {
		errInternal(w, s.Log, "list credentials", err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(offset + limit)
	}
	out := make([]credentialDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, toCredentialDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out, "next_cursor": next})
}

func (s *Server) loadCredential(w http.ResponseWriter, r *http.Request) (store.Credential, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return store.Credential{}, false
	}
	c, err := s.Store.GetCredential(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, "credential not found")
		return store.Credential{}, false
	}
	if err != nil {
		errInternal(w, s.Log, "get credential", err)
		return store.Credential{}, false
	}
	return c, true
}

func (s *Server) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCredential(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": toCredentialDTO(c)})
}

type patchCredentialReq struct {
	Status       *string             `json:"status"`
	Restrictions *creds.Restrictions `json:"restrictions"`
}

func (s *Server) handlePatchCredential(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCredential(w, r)
	if !ok {
		return
	}
	var req patchCredentialReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	var err error
	if req.Status != nil {
		switch *req.Status {
		case "active", "suspended", "revoked":
		default:
			errBadRequest(w, "invalid_status", "status must be active, suspended, or revoked")
			return
		}
		c, err = s.Store.UpdateCredentialStatus(r.Context(), store.UpdateCredentialStatusParams{ID: c.ID, Status: *req.Status})
		if err == nil {
			_ = s.Store.EmitEvent(r.Context(), c.DomainID, "credential.status_changed", map[string]any{
				"credential_id": c.ID.String(), "status": *req.Status,
			})
		}
	}
	if err == nil && req.Restrictions != nil {
		if verr := req.Restrictions.Validate(); verr != nil {
			errBadRequest(w, "invalid_restrictions", verr.Error())
			return
		}
		j, _ := req.Restrictions.JSON()
		c, err = s.Store.UpdateCredentialRestrictions(r.Context(), store.UpdateCredentialRestrictionsParams{ID: c.ID, Restrictions: j})
	}
	if err != nil {
		errInternal(w, s.Log, "patch credential", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": toCredentialDTO(c)})
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCredential(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteCredential(r.Context(), c.ID); err != nil {
		errInternal(w, s.Log, "delete credential", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), c.DomainID, "credential.deleted", map[string]any{"credential_id": c.ID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// handleCredentialStats returns windowed per-credential counts.
func (s *Server) handleCredentialStats(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCredential(w, r)
	if !ok {
		return
	}
	window, cutoff := parseWindow(r)
	st, err := s.Store.CredentialStats(r.Context(), store.CredentialStatsParams{CredentialID: &c.ID, CreatedAt: cutoff})
	if err != nil {
		errInternal(w, s.Log, "credential stats", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"credential_id": c.ID.String(),
		"window":        window,
		"stats": map[string]any{
			"submitted": st.Submitted, "delivered": st.Delivered, "deferred": st.Deferred,
			"bounced_hard": st.BouncedHard, "bounced_soft": st.BouncedSoft, "complaints": st.Complaints,
		},
	})
}

// validLocalPart is a conservative mailbox local-part check.
func validLocalPart(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '+' || c == '-'
		if !ok {
			return false
		}
	}
	return s[0] != '.' && s[len(s)-1] != '.'
}
