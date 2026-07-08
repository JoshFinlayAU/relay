package api

import (
	"net/http"

	"github.com/google/uuid"

	"relay/internal/retention"
)

// handleGetRetention returns the current message-retention policy: the
// WebUI-saved one if present, otherwise the default derived from static config.
func (s *Server) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	p, ok, err := retention.LoadPolicy(r.Context(), s.Store)
	if err != nil {
		errInternal(w, s.Log, "load retention policy", err)
		return
	}
	source := "custom"
	if !ok {
		source = "default"
		p = retention.Policy{
			Enabled:     s.RetentionDefaultEnabled,
			Mode:        retention.ModeAge,
			Days:        s.RetentionDefaultDays,
			MaxMessages: 100000,
		}
	}
	// Always offer both mode fields to the UI, even if one isn't active.
	if p.Days == 0 {
		p.Days = s.RetentionDefaultDays
	}
	if p.MaxMessages == 0 {
		p.MaxMessages = 100000
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": p, "source": source})
}

// handleSetRetention validates and persists a new retention policy.
func (s *Server) handleSetRetention(w http.ResponseWriter, r *http.Request) {
	var p retention.Policy
	if err := decodeJSON(r, &p); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		errBadRequest(w, "invalid_retention", err.Error())
		return
	}
	if err := retention.SavePolicy(r.Context(), s.Store, p); err != nil {
		errInternal(w, s.Log, "save retention policy", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), uuid.Nil, "settings.retention_updated", map[string]any{
		"enabled": p.Enabled, "mode": p.Mode, "days": p.Days, "max_messages": p.MaxMessages,
	})
	writeJSON(w, http.StatusOK, map[string]any{"policy": p, "source": "custom"})
}
