package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"relay/internal/store"
)

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	params := store.ListEventsParams{Limit: int32(limit + 1), Offset: int32(offset)}
	if v := r.URL.Query().Get("domain_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.DomainID = &id
		}
	}
	rows, err := s.Store.ListEvents(r.Context(), params)
	if err != nil {
		errInternal(w, s.Log, "list events", err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(offset + limit)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		var detail json.RawMessage = e.Detail
		if len(detail) == 0 {
			detail = json.RawMessage("{}")
		}
		out = append(out, map[string]any{
			"type":       e.Type,
			"domain_id":  uuidStr(e.DomainID),
			"detail":     detail,
			"created_at": tsPtr(e.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out, "next_cursor": next})
}
