package delivery

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"relay/internal/store"
)

func TestEffectiveMaxAgePerDomain(t *testing.T) {
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")

	// Domain with a 2h per-domain override.
	over := int32(2 * 3600)
	d1, err := testStore.CreateDomain(ctx, store.CreateDomainParams{Name: "override.example", VerifyToken: "t1", BounceSubdomain: "bounce.override.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.SetDomainDeliveryMaxAge(ctx, store.SetDomainDeliveryMaxAgeParams{ID: d1.ID, DeliveryMaxAgeSeconds: &over}); err != nil {
		t.Fatal(err)
	}
	// Domain with no override (NULL) → global default applies.
	d2, err := testStore.CreateDomain(ctx, store.CreateDomainParams{Name: "default.example", VerifyToken: "t2", BounceSubdomain: "bounce.default.example"})
	if err != nil {
		t.Fatal(err)
	}

	p := &Pool{Store: testStore, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Retry: RetryPolicy{Schedule: DefaultRetryPolicy().Schedule, MaxAge: 72 * time.Hour}}

	if got := p.effectiveMaxAge(ctx, &d1.ID); got != 2*time.Hour {
		t.Errorf("override domain maxAge = %v, want 2h", got)
	}
	if got := p.effectiveMaxAge(ctx, &d2.ID); got != 72*time.Hour {
		t.Errorf("default domain maxAge = %v, want 72h (global)", got)
	}
	if got := p.effectiveMaxAge(ctx, nil); got != 72*time.Hour {
		t.Errorf("nil domain maxAge = %v, want 72h (global)", got)
	}
}
