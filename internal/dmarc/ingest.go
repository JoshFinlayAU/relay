package dmarc

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/storage"
	"relay/internal/store"
)

// Ingester stores parsed DMARC reports, matched to a local domain by the
// report's policy domain. Reports for domains we don't host are ignored.
type Ingester struct {
	Store *store.Store
	Blobs *storage.Store
	Log   *slog.Logger
}

// Ingest extracts every report from a raw report email and persists those whose
// policy domain matches a hosted domain. Returns the number of reports stored.
func (ing *Ingester) Ingest(ctx context.Context, raw []byte) (int, error) {
	reports, err := ExtractFromEmail(raw)
	if err != nil {
		return 0, err
	}
	if len(reports) == 0 {
		return 0, nil
	}
	var rawRef *string
	if ing.Blobs != nil {
		if ref, err := ing.Blobs.Put(raw); err == nil {
			rawRef = &ref
		}
	}

	stored := 0
	for _, rep := range reports {
		d, err := ing.Store.GetDomainByName(ctx, rep.Domain)
		if err != nil {
			// Not a hosted domain — skip.
			continue
		}
		var policyP *string
		if rep.PolicyP != "" {
			p := rep.PolicyP
			policyP = &p
		}
		row, err := ing.Store.UpsertDMARCReport(ctx, store.UpsertDMARCReportParams{
			DomainID:  d.ID,
			OrgName:   rep.OrgName,
			ReportID:  rep.ReportID,
			DateBegin: ts(rep.Begin),
			DateEnd:   ts(rep.End),
			PolicyP:   policyP,
			PolicyPct: rep.PolicyPct,
			RawRef:    rawRef,
		})
		if err != nil {
			ing.Log.Warn("dmarc: upsert report", "err", err, "domain", rep.Domain)
			continue
		}
		if !row.Inserted {
			continue // already ingested; don't duplicate rows
		}
		for _, r := range rep.Rows {
			hf := r.HeaderFrom
			var hfp *string
			if hf != "" {
				hfp = &hf
			}
			_ = ing.Store.InsertDMARCRow(ctx, store.InsertDMARCRowParams{
				ReportID: row.ID, DomainID: d.ID, SourceIp: r.SourceIP,
				MessageCount: r.Count, Disposition: r.Disposition,
				DkimResult: r.DKIM, SpfResult: r.SPF, HeaderFrom: hfp, RowEnd: ts(rep.End),
			})
		}
		stored++
		ing.Log.Info("dmarc report ingested", "domain", rep.Domain, "org", rep.OrgName, "rows", len(rep.Rows))
		_ = ing.Store.EmitEvent(ctx, d.ID, "dmarc.report_ingested", map[string]any{
			"org": rep.OrgName, "report_id": rep.ReportID, "rows": len(rep.Rows),
		})
	}
	return stored, nil
}

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
