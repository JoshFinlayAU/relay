package stats

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/store"
)

var testStore *store.Store

func TestMain(m *testing.M) {
	url := os.Getenv("RELAY_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://relay:relay_dev_pw@127.0.0.1:5432/relay_test?sslmode=disable"
	}
	if err := store.Migrate(url); err != nil {
		panic("migrate: " + err.Error())
	}
	st, err := store.Connect(context.Background(), url, 5)
	if err != nil {
		panic("connect: " + err.Error())
	}
	testStore = st
	conn, _ := st.Pool.Acquire(context.Background())
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_lock(918273645)")
	code := m.Run()
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock(918273645)")
	conn.Release()
	st.Close()
	os.Exit(code)
}

func TestRollupMaterialises(t *testing.T) {
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")
	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{Name: "roll.example", VerifyToken: "t", BounceSubdomain: "bounce.roll.example"})
	if err != nil {
		t.Fatal(err)
	}
	did := d.ID
	msg, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: uuid.New(), Direction: "outbound", DomainID: &did, RcptTo: []string{"a@x.com"}, Status: "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A delivered attempt in the current hour.
	if err := testStore.InsertDeliveryAttempt(ctx, store.InsertDeliveryAttemptParams{
		MessageID: msg.ID, Rcpt: "a@x.com", Result: "delivered",
	}); err != nil {
		t.Fatal(err)
	}

	r := &Roller{Store: testStore, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r.tick(ctx)

	since := pgtype.Timestamptz{Time: time.Now().Add(-2 * time.Hour), Valid: true}
	rows, err := testStore.ListStatRollups(ctx, store.ListStatRollupsParams{DomainID: &did, Bucket: since})
	if err != nil {
		t.Fatal(err)
	}
	var submitted, delivered int64
	for _, b := range rows {
		submitted += b.Submitted
		delivered += b.Delivered
	}
	if submitted < 1 {
		t.Errorf("submitted rollup = %d, want >=1", submitted)
	}
	if delivered < 1 {
		t.Errorf("delivered rollup = %d, want >=1", delivered)
	}
}
