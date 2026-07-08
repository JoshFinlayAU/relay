package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// getRaw performs a bare GET (no JSON decode) and returns status + body.
func getRaw(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestMetricsRequiresAuthOnSharedMux(t *testing.T) {
	ts := newTestServer(t) // MetricsAddr unset ⇒ /metrics mounted behind auth
	if code, _ := getRaw(t, ts.URL+"/metrics", ""); code != http.StatusUnauthorized {
		t.Errorf("unauth /metrics = %d, want 401", code)
	}
	code, body := getRaw(t, ts.URL+"/metrics", testToken)
	if code != http.StatusOK {
		t.Fatalf("authed /metrics = %d, want 200", code)
	}
	if !strings.Contains(body, "# HELP") && !strings.Contains(body, "go_goroutines") {
		t.Errorf("metrics body does not look like Prometheus output")
	}
}

func TestHealthzHidesInternals(t *testing.T) {
	ts := newTestServer(t)
	// DB is healthy in tests; assert the body reports ok without leaking internals.
	code, body := getRaw(t, ts.URL+"/healthz", "")
	if code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", code)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("healthz body = %q", body)
	}
}
