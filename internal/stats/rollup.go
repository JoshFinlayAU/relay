// Package stats materialises hourly per-domain rollups into stat_rollups for
// dashboard time-series and NOCgenie alerting.
package stats

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/store"
)

// Roller periodically recomputes recent hourly rollup buckets.
type Roller struct {
	Store    *store.Store
	Log      *slog.Logger
	Interval time.Duration
}

// Run ticks until ctx is cancelled.
func (rl *Roller) Run(ctx context.Context) {
	if rl.Interval <= 0 {
		rl.Interval = 5 * time.Minute
	}
	// Prime immediately, then on each tick.
	rl.tick(ctx)
	t := time.NewTicker(rl.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rl.tick(ctx)
		}
	}
}

// tick recomputes the trailing few hourly buckets for domains active recently.
func (rl *Roller) tick(ctx context.Context) {
	since := pgtype.Timestamptz{Time: time.Now().Add(-3 * time.Hour), Valid: true}
	domains, err := rl.Store.DistinctActiveDomainIDs(ctx, since)
	if err != nil {
		rl.Log.Error("rollup: list domains", "err", err)
		return
	}
	hour := time.Now().UTC().Truncate(time.Hour)
	for _, d := range domains {
		if d == nil {
			continue
		}
		for i := 0; i < 3; i++ {
			bucket := pgtype.Timestamptz{Time: hour.Add(-time.Duration(i) * time.Hour), Valid: true}
			if err := rl.Store.UpsertStatRollup(ctx, store.UpsertStatRollupParams{Bucket: bucket, DomainID: *d}); err != nil {
				rl.Log.Error("rollup: upsert", "err", err, "domain", d.String())
			}
		}
	}
}
