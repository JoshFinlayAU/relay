// Package retention prunes stored message bodies and old metadata per the
// configured per-direction schedule (CLAUDE.md): outbound bodies after a TTL,
// inbound bodies once no webhook is pending and past a TTL, and message/event
// metadata after a longer window. Bodies are content-addressed, so a blob file
// is only removed once no message row still references it.
package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/storage"
	"relay/internal/store"
)

// Worker runs the periodic retention sweep.
type Worker struct {
	Store *store.Store
	Blobs *storage.Store
	Log   *slog.Logger

	Interval       time.Duration
	OutboundBodies time.Duration
	InboundBodies  time.Duration
	Metadata       time.Duration

	batch int32 // rows per body query (0 ⇒ default)
}

// Run sweeps once on start (after a short delay) then every Interval until ctx
// is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if w.Interval <= 0 {
		w.Interval = 6 * time.Hour
	}
	if w.batch <= 0 {
		w.batch = 500
	}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	// Initial pass shortly after boot so a long-running process reclaims space
	// without waiting a full interval.
	first := time.NewTimer(time.Minute)
	defer first.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			w.Sweep(ctx)
		case <-t.C:
			w.Sweep(ctx)
		}
	}
}

// Sweep performs one retention pass. Exported so it can be invoked directly and
// unit-tested.
func (w *Worker) Sweep(ctx context.Context) {
	if w.batch <= 0 {
		w.batch = 500
	}
	now := time.Now()
	outCut := ts(now.Add(-w.OutboundBodies))
	inCut := ts(now.Add(-w.InboundBodies))

	bodies := 0
	if w.OutboundBodies > 0 {
		rows, err := w.Store.ExpiredOutboundBodies(ctx, store.ExpiredOutboundBodiesParams{CreatedAt: outCut, Limit: w.batch})
		if err == nil {
			for _, r := range rows {
				if w.reapBody(ctx, r.ID, r.BodyRef) {
					bodies++
				}
			}
		} else {
			w.Log.Warn("retention: expired outbound query", "err", err)
		}
	}
	if w.InboundBodies > 0 {
		rows, err := w.Store.ExpiredInboundBodies(ctx, store.ExpiredInboundBodiesParams{CreatedAt: inCut, Limit: w.batch})
		if err == nil {
			for _, r := range rows {
				if w.reapBody(ctx, r.ID, r.BodyRef) {
					bodies++
				}
			}
		} else {
			w.Log.Warn("retention: expired inbound query", "err", err)
		}
	}

	var meta int64
	if w.Metadata > 0 {
		metaCut := ts(now.Add(-w.Metadata))
		if n, err := w.Store.DeleteOldMessages(ctx, metaCut); err == nil {
			meta += n
		} else {
			w.Log.Warn("retention: delete old messages", "err", err)
		}
		if n, err := w.Store.DeleteOldEvents(ctx, metaCut); err == nil {
			meta += n
		} else {
			w.Log.Warn("retention: delete old events", "err", err)
		}
	}
	if bodies > 0 || meta > 0 {
		w.Log.Info("retention sweep", "bodies_reclaimed", bodies, "metadata_rows_pruned", meta)
	}
}

// reapBody clears a message's body_ref and deletes the blob file iff no other
// message still references it. Returns true if a body was cleared.
func (w *Worker) reapBody(ctx context.Context, id uuid.UUID, ref *string) bool {
	if ref == nil {
		return false
	}
	if err := w.Store.ClearMessageBodyRef(ctx, id); err != nil {
		w.Log.Warn("retention: clear body_ref", "err", err, "msg", id)
		return false
	}
	// Only delete the shared blob when no remaining row points at it.
	if n, err := w.Store.CountBodyRefUsers(ctx, ref); err == nil && n == 0 {
		if err := w.Blobs.Delete(*ref); err != nil {
			w.Log.Warn("retention: delete blob", "err", err, "ref", *ref)
		}
	}
	return true
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
