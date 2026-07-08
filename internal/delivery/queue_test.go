package delivery

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/store"
)

func uuidNew(t *testing.T) uuid.UUID { t.Helper(); return uuid.New() }
func rcpt(i int) string              { return fmt.Sprintf("r%d@example.net", i) }

var testStore *store.Store

func TestMain(m *testing.M) {
	url := os.Getenv("RELAY_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://relay:relay_dev_pw@127.0.0.1:5432/relay_test?sslmode=disable"
	}
	if err := store.Migrate(url); err != nil {
		panic("migrate: " + err.Error())
	}
	st, err := store.Connect(context.Background(), url, 8)
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

// seedJobs creates a domain, a message, and n queued delivery jobs.
func seedJobs(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	_, _ = testStore.Pool.Exec(ctx, "TRUNCATE domains CASCADE")
	d, err := testStore.CreateDomain(ctx, store.CreateDomainParams{Name: "deliv.example", VerifyToken: "t", BounceSubdomain: "bounce.deliv.example"})
	if err != nil {
		t.Fatal(err)
	}
	did := d.ID
	body := "msgs/aa/bb/deadbeef"
	verp := "bounce-x@bounce.deliv.example"
	msg, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: uuidNew(t), Direction: "outbound", DomainID: &did, MailFrom: &verp, BodyRef: &body, Status: "queued",
		RcptTo: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := testStore.EnqueueDeliveryJob(ctx, store.EnqueueDeliveryJobParams{MessageID: msg.ID, Rcpt: rcpt(i)}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestOpenRelayNotApplicable is a placeholder so `go test -run OpenRelay` in CI
// finds a target in this package too (real open-relay tests live in submission).
func TestOpenRelayNotApplicable(t *testing.T) {}

// TestClaimNoDoubleClaim proves SKIP LOCKED hands each job to exactly one worker.
func TestClaimNoDoubleClaim(t *testing.T) {
	seedJobs(t, 50)
	ctx := context.Background()

	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		id := "worker-" + rcpt(w)
		go func() {
			defer wg.Done()
			for {
				jobs, err := testStore.ClaimDeliveryJobs(ctx, store.ClaimDeliveryJobsParams{LockedBy: &id, Limit: 3})
				if err != nil || len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					seen[j.ID.String()]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(seen) != 50 {
		t.Fatalf("claimed %d distinct jobs, want 50", len(seen))
	}
	for id, c := range seen {
		if c != 1 {
			t.Errorf("job %s claimed %d times (double claim!)", id, c)
		}
	}
}

// TestRequeueStaleJobs recovers jobs whose worker died mid-flight.
func TestRequeueStaleJobs(t *testing.T) {
	seedJobs(t, 3)
	ctx := context.Background()
	id := "dead-worker"
	claimed, err := testStore.ClaimDeliveryJobs(ctx, store.ClaimDeliveryJobsParams{LockedBy: &id, Limit: 3})
	if err != nil || len(claimed) != 3 {
		t.Fatalf("claim: %v n=%d", err, len(claimed))
	}
	// Nothing is claimable now (all in_progress).
	none, _ := testStore.ClaimDeliveryJobs(ctx, store.ClaimDeliveryJobsParams{LockedBy: &id, Limit: 3})
	if len(none) != 0 {
		t.Fatalf("expected no claimable jobs, got %d", len(none))
	}
	// Simulate crash: requeue jobs locked before "now".
	if err := testStore.RequeueStaleJobs(ctx, pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true}); err != nil {
		t.Fatal(err)
	}
	again, _ := testStore.ClaimDeliveryJobs(ctx, store.ClaimDeliveryJobsParams{LockedBy: &id, Limit: 3})
	if len(again) != 3 {
		t.Fatalf("after requeue expected 3 reclaimable, got %d", len(again))
	}
}

func TestJobStatusCounts(t *testing.T) {
	seedJobs(t, 3)
	ctx := context.Background()
	id := "w"
	jobs, _ := testStore.ClaimDeliveryJobs(ctx, store.ClaimDeliveryJobsParams{LockedBy: &id, Limit: 3})
	_ = testStore.MarkJobDelivered(ctx, store.MarkJobDeliveredParams{ID: jobs[0].ID})
	_ = testStore.FailJob(ctx, store.FailJobParams{ID: jobs[1].ID})
	counts, err := testStore.JobStatusCounts(ctx, jobs[0].MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Delivered != 1 || counts.Failed != 1 || counts.Pending != 1 {
		t.Errorf("counts = %+v", counts)
	}
}
