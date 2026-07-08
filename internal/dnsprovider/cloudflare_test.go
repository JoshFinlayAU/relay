package dnsprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseMX(t *testing.T) {
	p, h := parseMX("10 mail.as135559.net.au.")
	if p != 10 || h != "mail.as135559.net.au" {
		t.Errorf("parseMX = %d %q", p, h)
	}
	// Fallback when no priority present.
	if p2, h2 := parseMX("mail.example.com"); p2 != 10 || h2 != "mail.example.com" {
		t.Errorf("parseMX fallback = %d %q", p2, h2)
	}
}

// A 403 (e.g. token lacking Zone:Read) must surface Cloudflare's real error and
// the Zone:Read + DNS:Edit hint, not a generic guess.
func TestDoForbiddenSurfacesRealError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Unauthorized to access requested resource"}]}`))
	}))
	defer srv.Close()

	c := &cfClient{token: "t", hc: &http.Client{Timeout: 5 * time.Second}}
	err := c.do(context.Background(), http.MethodGet, srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected an error on 403")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Unauthorized to access requested resource") {
		t.Errorf("error should surface Cloudflare's message, got: %s", msg)
	}
	if !strings.Contains(msg, "Zone:Read") || !strings.Contains(msg, "DNS:Edit") {
		t.Errorf("error should hint at Zone:Read + DNS:Edit, got: %s", msg)
	}
}
