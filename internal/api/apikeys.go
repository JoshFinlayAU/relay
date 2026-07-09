package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"relay/internal/store"
)

type apiKeyDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt *time.Time `json:"created_at"`
	LastUsed  *time.Time `json:"last_used"`
	Revoked   bool       `json:"revoked"`
}

type createAPIKeyReq struct {
	Name string `json:"name"`
}

// handleCreateAPIKey mints a new API key. The secret is returned exactly once;
// only its SHA-256 is stored.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		errBadRequest(w, "invalid_name", "name is required (<=100 chars)")
		return
	}
	raw, err := randToken()
	if err != nil {
		errInternal(w, s.Log, "gen api key", err)
		return
	}
	secret := "relay_" + raw
	tok, err := s.Store.CreateAPIToken(r.Context(), store.CreateAPITokenParams{
		Name: name, TokenHash: sha256hex(secret),
	})
	if err != nil {
		errInternal(w, s.Log, "create api token", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), uuid.Nil, "api_key.created", map[string]any{"id": tok.ID.String(), "name": name})
	// Secret shown once, like credential secrets.
	writeJSON(w, http.StatusCreated, map[string]any{
		"api_key": apiKeyDTO{ID: tok.ID.String(), Name: tok.Name, CreatedAt: tsPtr(tok.CreatedAt)},
		"token":   secret,
	})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListAPITokens(r.Context())
	if err != nil {
		errInternal(w, s.Log, "list api tokens", err)
		return
	}
	out := make([]apiKeyDTO, 0, len(rows))
	for _, t := range rows {
		out = append(out, apiKeyDTO{
			ID: t.ID.String(), Name: t.Name,
			CreatedAt: tsPtr(t.CreatedAt), LastUsed: tsPtr(t.LastUsed),
			Revoked: t.RevokedAt.Valid,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": out})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	n, err := s.Store.RevokeAPIToken(r.Context(), id)
	if err != nil {
		errInternal(w, s.Log, "revoke api token", err)
		return
	}
	if n == 0 {
		errNotFound(w, "api key not found or already revoked")
		return
	}
	_ = s.Store.EmitEvent(r.Context(), uuid.Nil, "api_key.revoked", map[string]any{"id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}
