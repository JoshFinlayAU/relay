package api

import (
	"net/http"
	"strings"
	"time"

	"relay/internal/store"
)

type suppressionDTO struct {
	Address   string     `json:"address"`
	Reason    *string    `json:"reason"`
	CreatedAt *time.Time `json:"created_at"`
}

func (s *Server) handleListSuppressions(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	limit, offset := parsePagination(r)
	rows, err := s.Store.ListSuppressions(r.Context(), store.ListSuppressionsParams{
		DomainID: d.ID, Limit: int32(limit + 1), Offset: int32(offset),
	})
	if err != nil {
		errInternal(w, s.Log, "list suppressions", err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(offset + limit)
	}
	out := make([]suppressionDTO, 0, len(rows))
	for _, sp := range rows {
		out = append(out, suppressionDTO{Address: sp.Address, Reason: sp.Reason, CreatedAt: tsPtr(sp.CreatedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"suppressions": out, "next_cursor": next})
}

type addSuppressionReq struct {
	Address string `json:"address"`
	Reason  string `json:"reason"`
}

func (s *Server) handleAddSuppression(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	var req addSuppressionReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	addr := strings.ToLower(strings.TrimSpace(req.Address))
	if !strings.Contains(addr, "@") {
		errBadRequest(w, "invalid_address", "address must be an email address")
		return
	}
	reason := req.Reason
	if reason == "" {
		reason = "manual"
	}
	sp, err := s.Store.AddSuppression(r.Context(), store.AddSuppressionParams{DomainID: d.ID, Address: addr, Reason: &reason})
	if err != nil {
		errInternal(w, s.Log, "add suppression", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), d.ID, "suppression.added", map[string]any{"address": addr, "reason": reason})
	writeJSON(w, http.StatusCreated, map[string]any{"suppression": suppressionDTO{Address: sp.Address, Reason: sp.Reason, CreatedAt: tsPtr(sp.CreatedAt)}})
}

// handleRemoveSuppression removes/overrides a suppressed address.
func (s *Server) handleRemoveSuppression(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	addr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("address")))
	if addr == "" {
		errBadRequest(w, "missing_address", "address query parameter required")
		return
	}
	if err := s.Store.RemoveSuppression(r.Context(), store.RemoveSuppressionParams{DomainID: d.ID, Address: addr}); err != nil {
		errInternal(w, s.Log, "remove suppression", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), d.ID, "suppression.removed", map[string]any{"address": addr})
	w.WriteHeader(http.StatusNoContent)
}
