package delivery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"relay/internal/storage"
	"relay/internal/store"
)

// TestDeliveryThroughput drives the delivery pool against an in-process sink and
// asserts it sustains well above the 50 msg/s production target with no queue
// growth. (Uses a fast local sink; exercises claim→deliver→record→status.)
func TestDeliveryThroughput(t *testing.T) {
	// Opt-in: a throughput test is sensitive to CPU contention from the rest of
	// the suite, so it runs only when explicitly requested (`make loadtest`).
	if os.Getenv("RELAY_LOAD_TEST") == "" {
		t.Skip("set RELAY_LOAD_TEST=1 to run the throughput load test")
	}
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")

	mx := &fakeMX{}
	host, port, stop := startFakeMX(t, mx)
	defer stop()

	blobs, _ := storage.New(t.TempDir())
	ref, _ := blobs.Put([]byte("From: a@load.test\r\nSubject: load\r\n\r\nbody\r\n"))

	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{Name: "load.test", VerifyToken: "t", BounceSubdomain: "bounce.load.test"})
	if err != nil {
		t.Fatal(err)
	}
	did := d.ID

	const n = 200
	for i := 0; i < n; i++ {
		msg, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
			ID: uuid.New(), Direction: "outbound", DomainID: &did, MailFrom: strptr("b@load.test"),
			RcptTo: []string{"u@load.test"}, BodyRef: &ref, Status: "queued",
		})
		if err != nil {
			t.Fatal(err)
		}
		// Distinct destination domains so the per-domain cap doesn't serialise.
		if _, err := testStore.EnqueueDeliveryJob(ctx, store.EnqueueDeliveryJobParams{MessageID: msg.ID, Rcpt: fmt.Sprintf("u@d%d.test", i)}); err != nil {
			t.Fatal(err)
		}
	}

	pool := &Pool{
		Store: testStore, Blobs: blobs, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hostname: "mail.test", WorkerID: "load", Concurrency: 16, PerDomain: 16,
		Sink: host + ":" + port, Timeout: 5 * time.Second,
	}
	pctx, cancel := context.WithCancel(ctx)
	go pool.Run(pctx)

	start := time.Now()
	deadline := time.Now().Add(20 * time.Second)
	for {
		depth, _ := testStore.QueueDepth(ctx)
		if depth == 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("queue did not drain in time; depth=%d", depth)
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(start)
	cancel()

	rate := float64(n) / elapsed.Seconds()
	t.Logf("delivered %d messages in %s = %.0f msg/s", n, elapsed.Round(time.Millisecond), rate)
	if rate < 50 {
		t.Errorf("throughput %.0f msg/s below the 50 msg/s target", rate)
	}
	// Sink actually received them all.
	mx.mu.Lock()
	got := len(mx.received)
	mx.mu.Unlock()
	if got != n {
		t.Errorf("sink received %d, want %d", got, n)
	}
}

func strptr(s string) *string { return &s }
