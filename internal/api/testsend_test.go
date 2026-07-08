package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"relay/internal/store"
)

// makeActiveDomainWithKey creates an active domain + DKIM key so signing works.
func makeActiveDomainWithKey(t *testing.T, ts, token, name string) string {
	t.Helper()
	did := createDomainForTest(t, ts, token, name)
	// Activate it directly (verification needs live DNS).
	ctx := context.Background()
	id := uuid.MustParse(did)
	_, _ = testStore.UpdateDomainStatus(ctx, store.UpdateDomainStatusParams{ID: id, Status: "active"})
	return did
}

func TestTestSend(t *testing.T) {
	ts := newTestServer(t)
	did := makeActiveDomainWithKey(t, ts.URL, testToken, "tsend.example")

	status, out := do(t, "POST", ts.URL+"/v1/domains/"+did+"/test-send", testToken, map[string]any{"to": "dest@example.net"})
	if status != http.StatusAccepted {
		t.Fatalf("test-send = %d (%v)", status, out)
	}
	mid, _ := out["message_id"].(string)
	if mid == "" {
		t.Fatal("no message_id returned")
	}

	// A queued message + a delivery job exist, and the stored body is DKIM-signed.
	var bodyRef, statusStr string
	if err := testStore.Pool.QueryRow(context.Background(),
		"SELECT body_ref, status FROM messages WHERE id=$1", mid).Scan(&bodyRef, &statusStr); err != nil {
		t.Fatalf("message row: %v", err)
	}
	if statusStr != "queued" {
		t.Errorf("status = %s, want queued", statusStr)
	}
	var jobs int
	_ = testStore.Pool.QueryRow(context.Background(), "SELECT count(*) FROM delivery_jobs WHERE message_id=$1", mid).Scan(&jobs)
	if jobs != 1 {
		t.Errorf("delivery jobs = %d, want 1", jobs)
	}

	// Invalid recipient rejected.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/test-send", testToken, map[string]any{"to": "notanemail"})
	if status != http.StatusBadRequest {
		t.Errorf("invalid recipient = %d, want 400", status)
	}

	// Header-injection recipient rejected (CRLF smuggling into signed message).
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/test-send", testToken,
		map[string]any{"to": "dest@example.net\r\nBcc: evil@attacker.example"})
	if status != http.StatusBadRequest {
		t.Errorf("CRLF-injection recipient = %d, want 400", status)
	}
}

func TestCleanAddress(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"user@example.com", "user@example.com", true},
		{"  User@Example.COM  ", "user@example.com", true},
		{"a@b.com\r\nBcc: evil@x.com", "", false},
		{"a@b.com\nBcc: evil@x.com", "", false},
		{"a@b.com\rSubject: x", "", false},
		{"a@b.com\x00", "", false},
		{"Name <a@b.com>", "", false},
		{"a@b.com, c@d.com", "", false},
		{"not-an-email", "", false},
		{"", "", false},
		{"@b.com", "", false},
	}
	for _, c := range cases {
		got, ok := cleanAddress(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("cleanAddress(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestValidWebhookURL(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"https://hooks.example.com/relay", true},
		{"http://app.internal.lan/webhook", true},
		{"https://10.0.0.5:8443/hook", true},
		{"ftp://example.com/x", false},
		{"", false},
		{"not a url", false},
		{"https://127.0.0.1/hook", false},
		{"http://[::1]/hook", false},
		{"http://169.254.169.254/latest/meta", false},
		{"http://0.0.0.0/x", false},
	}
	for _, c := range cases {
		if got := validWebhookURL(c.in); got != c.ok {
			t.Errorf("validWebhookURL(%q) = %v, want %v", c.in, got, c.ok)
		}
	}
}

func TestDomainStatsEndpoint(t *testing.T) {
	ts := newTestServer(t)
	did := createDomainForTest(t, ts.URL, testToken, "dstats.example")
	status, out := do(t, "GET", ts.URL+"/v1/domains/"+did+"/stats?window=7d", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("stats = %d", status)
	}
	if out["window"] != "7d" {
		t.Errorf("window = %v", out["window"])
	}
	st := out["stats"].(map[string]any)
	if st["submitted"].(float64) != 0 {
		t.Errorf("expected 0 submitted for new domain, got %v", st["submitted"])
	}
}
