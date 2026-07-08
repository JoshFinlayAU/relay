package retention

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"relay/internal/storage"
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

func TestRetentionSweep(t *testing.T) {
	ctx := context.Background()
	blobsDir := t.TempDir()
	blobs, err := storage.New(blobsDir)
	if err != nil {
		t.Fatal(err)
	}
	// Clear message table to isolate this test.
	_, _ = testStore.Pool.Exec(ctx, "DELETE FROM messages")

	// Old outbound message with a stored body → should be reaped.
	oldRef, _ := blobs.Put([]byte("old outbound body"))
	oldID := uuid.New()
	insertMsg(t, oldID, "outbound", oldRef, time.Now().Add(-30*24*time.Hour))

	// Recent outbound message → body must be kept.
	newRef, _ := blobs.Put([]byte("fresh outbound body"))
	newID := uuid.New()
	insertMsg(t, newID, "outbound", newRef, time.Now())

	w := &Worker{
		Store: testStore, Blobs: blobs, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		OutboundBodies: 7 * 24 * time.Hour,
		InboundBodies:  7 * 24 * time.Hour,
		Metadata:       0, // don't prune rows in this test
	}
	w.Sweep(ctx)

	if r := bodyRef(t, oldID); r != nil {
		t.Errorf("old message body_ref = %v, want NULL", *r)
	}
	if _, err := blobs.Get(oldRef); err == nil {
		t.Error("old blob should have been deleted")
	}
	if r := bodyRef(t, newID); r == nil {
		t.Error("recent message body_ref was cleared, should be kept")
	}
	if _, err := blobs.Get(newRef); err != nil {
		t.Error("recent blob should still exist")
	}
}

func TestRetentionKeepsSharedBlob(t *testing.T) {
	ctx := context.Background()
	blobs, _ := storage.New(t.TempDir())
	_, _ = testStore.Pool.Exec(ctx, "DELETE FROM messages")

	// Two messages share one content-addressed body; only one is expired.
	ref, _ := blobs.Put([]byte("shared body"))
	oldID, newID := uuid.New(), uuid.New()
	insertMsg(t, oldID, "outbound", ref, time.Now().Add(-30*24*time.Hour))
	insertMsg(t, newID, "outbound", ref, time.Now())

	w := &Worker{Store: testStore, Blobs: blobs, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		OutboundBodies: 7 * 24 * time.Hour, InboundBodies: 7 * 24 * time.Hour}
	w.Sweep(ctx)

	// The blob is still referenced by newID, so it must NOT be deleted.
	if _, err := blobs.Get(ref); err != nil {
		t.Error("shared blob deleted while still referenced")
	}
}

func insertMsg(t *testing.T, id uuid.UUID, dir, ref string, created time.Time) {
	t.Helper()
	_, err := testStore.Pool.Exec(context.Background(),
		`INSERT INTO messages (id, direction, rcpt_to, body_ref, status, created_at, queued_at)
		 VALUES ($1,$2,'{}',$3,'queued',$4,$4)`, id, dir, ref, created)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func bodyRef(t *testing.T, id uuid.UUID) *string {
	t.Helper()
	var ref *string
	if err := testStore.Pool.QueryRow(context.Background(),
		"SELECT body_ref FROM messages WHERE id=$1", id).Scan(&ref); err != nil {
		t.Fatalf("query body_ref: %v", err)
	}
	return ref
}
