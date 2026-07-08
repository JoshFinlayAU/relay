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

	// Message + delivery history pruning, governed by the runtime policy (WebUI)
	// with the static config metadata age as the fallback.
	meta := w.pruneMessages(ctx, now)

	// Events are pruned on the config metadata age (independent of the mail policy).
	if w.Metadata > 0 {
		if n, err := w.Store.DeleteOldEvents(ctx, ts(now.Add(-w.Metadata))); err != nil {
			w.Log.Warn("retention: delete old events", "err", err)
		} else {
			meta += n
		}
	}
	if bodies > 0 || meta > 0 {
		w.Log.Info("retention sweep", "bodies_reclaimed", bodies, "messages_pruned", meta)
	}
}

// pruneMessages deletes message rows (cascading to delivery jobs/attempts/
// bounces) per the effective policy, cleaning up any now-unreferenced blobs.
func (w *Worker) pruneMessages(ctx context.Context, now time.Time) int64 {
	p := w.effectivePolicy(ctx)
	if !p.Enabled {
		return 0
	}
	var pruned int64
	for {
		var (
			ids  []uuid.UUID
			refs []string
		)
		switch p.Mode {
		case ModeAge:
			rows, err := w.Store.MessagesOlderThan(ctx, store.MessagesOlderThanParams{
				CreatedAt: ts(now.AddDate(0, 0, -p.Days)), Limit: w.batch,
			})
			if err != nil {
				w.Log.Warn("retention: messages older-than query", "err", err)
				return pruned
			}
			for _, r := range rows {
				ids = append(ids, r.ID)
				if r.BodyRef != nil {
					refs = append(refs, *r.BodyRef)
				}
			}
		case ModeCount:
			rows, err := w.Store.MessagesBeyondCount(ctx, store.MessagesBeyondCountParams{
				Keep: int32(p.MaxMessages), Lim: w.batch,
			})
			if err != nil {
				w.Log.Warn("retention: messages beyond-count query", "err", err)
				return pruned
			}
			for _, r := range rows {
				ids = append(ids, r.ID)
				if r.BodyRef != nil {
					refs = append(refs, *r.BodyRef)
				}
			}
		default:
			return pruned
		}
		if len(ids) == 0 {
			return pruned
		}
		n, err := w.Store.DeleteMessagesByIDs(ctx, ids)
		if err != nil {
			w.Log.Warn("retention: delete messages", "err", err)
			return pruned
		}
		pruned += n
		// Delete blobs of removed messages that no surviving row references.
		for _, ref := range dedupe(refs) {
			r := ref
			if c, err := w.Store.CountBodyRefUsers(ctx, &r); err == nil && c == 0 {
				if err := w.Blobs.Delete(r); err != nil {
					w.Log.Warn("retention: delete blob", "err", err, "ref", r)
				}
			}
		}
		if int32(len(ids)) < w.batch {
			return pruned
		}
	}
}

// effectivePolicy is the WebUI-set policy if present, else the config metadata
// age (both default to age-based).
func (w *Worker) effectivePolicy(ctx context.Context) Policy {
	if p, ok, err := LoadPolicy(ctx, w.Store); err != nil {
		w.Log.Warn("retention: load policy", "err", err)
	} else if ok {
		return p
	}
	if w.Metadata <= 0 {
		return Policy{Enabled: false}
	}
	return Policy{Enabled: true, Mode: ModeAge, Days: int(w.Metadata.Hours() / 24)}
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
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
