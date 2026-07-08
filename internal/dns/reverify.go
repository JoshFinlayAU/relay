package dns

import (
	"context"
	"log/slog"
	"time"

	"relay/internal/store"
)

// Reverifier periodically re-checks active/degraded domains and flips status on
// failure (active → degraded) or recovery (degraded → active), emitting events.
type Reverifier struct {
	Store    *store.Store
	Verifier *Verifier
	Log      *slog.Logger
	Interval time.Duration
}

// Run blocks until ctx is cancelled, re-verifying on each tick.
func (rv *Reverifier) Run(ctx context.Context) {
	if rv.Interval <= 0 {
		rv.Interval = 6 * time.Hour
	}
	t := time.NewTicker(rv.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rv.tick(ctx)
		}
	}
}

func (rv *Reverifier) tick(ctx context.Context) {
	domains, err := rv.Store.ListActiveDomains(ctx)
	if err != nil {
		rv.Log.Error("reverify: list domains", "err", err)
		return
	}
	for _, d := range domains {
		select {
		case <-ctx.Done():
			return
		default:
		}
		rv.checkOne(ctx, d)
	}
}

func (rv *Reverifier) checkOne(ctx context.Context, d store.Domain) {
	key, err := rv.Store.GetActiveDKIMKey(ctx, d.ID)
	if err != nil {
		rv.Log.Warn("reverify: no dkim key", "domain", d.Name, "err", err)
		return
	}
	results, err := rv.Verifier.Verify(ctx, VerifyInput{
		Domain:     d.Name,
		Token:      d.VerifyToken,
		Selector:   key.Selector,
		DKIMPubB64: key.PublicKey,
		Receiving:  d.Receiving,
	})
	if err != nil {
		rv.Log.Warn("reverify: verify failed", "domain", d.Name, "err", err)
		return
	}
	req := RequiredPurposes(d.Receiving)
	allPass := true
	for _, res := range results {
		_ = rv.Store.UpdateDNSRecordResult(ctx, store.UpdateDNSRecordResultParams{
			DomainID:      d.ID,
			Purpose:       string(res.Purpose),
			ObservedValue: nilIfEmpty(res.Observed),
			LastResult:    string(res.Result),
			Detail:        nilIfEmpty(res.Detail),
		})
		// pass OR warn satisfies a required record; only a hard fail degrades.
		if req[res.Purpose] && res.Result == ResultFail {
			allPass = false
		}
	}

	var newStatus string
	switch {
	case allPass && d.Status == "degraded":
		newStatus = "active"
	case !allPass && d.Status == "active":
		newStatus = "degraded"
	default:
		return // no transition
	}
	if _, err := rv.Store.UpdateDomainStatus(ctx, store.UpdateDomainStatusParams{ID: d.ID, Status: newStatus}); err != nil {
		rv.Log.Error("reverify: status update", "domain", d.Name, "err", err)
		return
	}
	_ = rv.Store.EmitEvent(ctx, d.ID, "domain.status_changed", map[string]any{"status": newStatus, "source": "reverifier"})
	rv.Log.Info("reverify: status changed", "domain", d.Name, "status", newStatus)
}

// RequiredPurposes is the set of records that must pass for a domain to be
// active (DMARC is recommended, not required).
func RequiredPurposes(receiving bool) map[Purpose]bool {
	req := map[Purpose]bool{
		PurposeOwnership: true,
		PurposeDKIM:      true,
		PurposeSPF:       true,
		PurposeBounceMX:  true,
	}
	if receiving {
		req[PurposeInboundMX] = true
	}
	return req
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
