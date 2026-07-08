package api

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/store"
)

// parseWindow maps ?window=24h|7d|30d to a label + cutoff timestamp (default 24h).
func parseWindow(r *http.Request) (string, pgtype.Timestamptz) {
	window := r.URL.Query().Get("window")
	var d time.Duration
	switch window {
	case "7d":
		d = 7 * 24 * time.Hour
	case "30d":
		d = 30 * 24 * time.Hour
	default:
		window = "24h"
		d = 24 * time.Hour
	}
	return window, pgtype.Timestamptz{Time: time.Now().Add(-d), Valid: true}
}

func (s *Server) handleDomainStats(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	window, cutoff := parseWindow(r)
	st, err := s.Store.DomainStats(r.Context(), store.DomainStatsParams{DomainID: &d.ID, CreatedAt: cutoff})
	if err != nil {
		errInternal(w, s.Log, "domain stats", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain_id": d.ID.String(),
		"window":    window,
		"stats": map[string]any{
			"submitted": st.Submitted, "delivered": st.Delivered, "deferred": st.Deferred,
			"bounced_hard": st.BouncedHard, "bounced_soft": st.BouncedSoft, "complaints": st.Complaints,
		},
	})
}

// handleDomainTimeseries returns hourly rollup buckets for charts.
func (s *Server) handleDomainTimeseries(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	_, cutoff := parseWindow(r)
	rows, err := s.Store.ListStatRollups(r.Context(), store.ListStatRollupsParams{DomainID: &d.ID, Bucket: cutoff})
	if err != nil {
		errInternal(w, s.Log, "timeseries", err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		out = append(out, map[string]any{
			"bucket": tsPtr(b.Bucket), "submitted": b.Submitted, "delivered": b.Delivered,
			"deferred": b.Deferred, "bounced_hard": b.BouncedHard, "bounced_soft": b.BouncedSoft,
			"complaints": b.Complaints,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain_id": d.ID.String(), "buckets": out})
}
