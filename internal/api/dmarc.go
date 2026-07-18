package api

import (
	"net/http"

	"relay/internal/store"
)

// handleDomainDMARC returns the per-domain DMARC analyzer view: windowed
// aggregate totals, top sending sources, and the recent report list.
func (s *Server) handleDomainDMARC(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	window, cutoff := parseWindow(r)
	ctx := r.Context()

	sum, err := s.Store.DMARCSummary(ctx, store.DMARCSummaryParams{DomainID: d.ID, RowEnd: cutoff})
	if err != nil {
		errInternal(w, s.Log, "dmarc summary", err)
		return
	}
	srcRows, _ := s.Store.DMARCTopSources(ctx, store.DMARCTopSourcesParams{DomainID: d.ID, RowEnd: cutoff, Limit: 20})
	sources := make([]map[string]any, 0, len(srcRows))
	for _, sr := range srcRows {
		sources = append(sources, map[string]any{"source_ip": sr.SourceIp, "total": sr.Total, "passed": sr.Passed})
	}
	repRows, _ := s.Store.ListDMARCReports(ctx, store.ListDMARCReportsParams{DomainID: d.ID, Limit: 50, Offset: 0})
	reports := make([]map[string]any, 0, len(repRows))
	for _, rr := range repRows {
		reports = append(reports, map[string]any{
			"org_name": rr.OrgName, "report_id": rr.ReportID,
			"date_begin": tsPtr(rr.DateBegin), "date_end": tsPtr(rr.DateEnd),
			"policy_p": rr.PolicyP, "policy_pct": rr.PolicyPct,
			"messages": rr.Messages, "received_at": tsPtr(rr.ReceivedAt),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"window": window,
		"summary": map[string]any{
			"total": sum.Total, "passed": sum.Passed,
			"dkim_pass": sum.DkimPass, "spf_pass": sum.SpfPass,
			"quarantined": sum.Quarantined, "rejected": sum.Rejected,
		},
		"top_sources": sources,
		"reports":     reports,
	})
}
