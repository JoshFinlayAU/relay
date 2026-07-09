package api

import (
	"net/http"
	"testing"
)

func TestAPIKeyLifecycle(t *testing.T) {
	ts := newTestServer(t)

	// Create a key (authenticated with the static test token).
	status, out := do(t, "POST", ts.URL+"/v1/api-keys", testToken, map[string]any{"name": "ci-bot"})
	if status != http.StatusCreated {
		t.Fatalf("create api key = %d (%v)", status, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("no token returned")
	}
	keyID := out["api_key"].(map[string]any)["id"].(string)

	// The new key authenticates a normal endpoint.
	if status, _ := do(t, "GET", ts.URL+"/v1/domains", token, nil); status != http.StatusOK {
		t.Errorf("GET /v1/domains with api key = %d, want 200", status)
	}

	// It can onboard a domain end-to-end via the API.
	st, dres := do(t, "POST", ts.URL+"/v1/domains", token, map[string]any{"name": "apikey-onboard.example"})
	if st != http.StatusCreated {
		t.Fatalf("onboard via api key = %d (%v)", st, dres)
	}
	if dns, ok := dres["dns"].([]any); !ok || len(dns) == 0 {
		t.Errorf("onboard response should include dns records, got %v", dres["dns"])
	}

	// The secret is never returned again by the list endpoint.
	_, list := do(t, "GET", ts.URL+"/v1/api-keys", testToken, nil)
	for _, k := range list["api_keys"].([]any) {
		if _, leaked := k.(map[string]any)["token"]; leaked {
			t.Error("list must not expose the token")
		}
	}

	// Revoke it; the token no longer authenticates.
	if status, _ := do(t, "DELETE", ts.URL+"/v1/api-keys/"+keyID, testToken, nil); status != http.StatusNoContent {
		t.Errorf("revoke = %d, want 204", status)
	}
	if status, _ := do(t, "GET", ts.URL+"/v1/domains", token, nil); status != http.StatusUnauthorized {
		t.Errorf("revoked key = %d, want 401", status)
	}
}
