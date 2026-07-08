package api

import (
	"net/http"
	"testing"
)

// createDomainForTest returns a new domain id.
func createDomainForTest(t *testing.T, ts, token, name string) string {
	t.Helper()
	status, out := do(t, "POST", ts+"/v1/domains", token, map[string]any{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("create domain %d", status)
	}
	return out["domain"].(map[string]any)["id"].(string)
}

func TestCredentialLifecycle(t *testing.T) {
	ts := newTestServer(t)
	did := createDomainForTest(t, ts.URL, testToken, "creds.example")

	// Create credential - secret returned exactly once.
	status, out := do(t, "POST", ts.URL+"/v1/domains/"+did+"/credentials", testToken, map[string]any{
		"name": "orders",
		"restrictions": map[string]any{
			"allowed_from":   []string{"orders@creds.example"},
			"max_recipients": 50,
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create credential %d (%v)", status, out)
	}
	secret, _ := out["secret"].(string)
	if secret == "" {
		t.Fatal("secret not returned on creation")
	}
	cred := out["credential"].(map[string]any)
	if cred["username"] != "orders@creds.example" {
		t.Errorf("username = %v", cred["username"])
	}
	if _, leaked := cred["secret_hash"]; leaked {
		t.Error("secret_hash leaked in response")
	}
	cid := cred["id"].(string)

	// Duplicate username → 409.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/credentials", testToken, map[string]any{"name": "orders"})
	if status != http.StatusConflict {
		t.Errorf("duplicate credential = %d, want 409", status)
	}

	// Invalid name → 400.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/credentials", testToken, map[string]any{"name": "bad name!"})
	if status != http.StatusBadRequest {
		t.Errorf("invalid name = %d, want 400", status)
	}

	// GET credential - never returns the secret.
	status, out = do(t, "GET", ts.URL+"/v1/credentials/"+cid, testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get credential %d", status)
	}
	if _, leaked := out["secret"]; leaked {
		t.Error("secret retrievable after creation - must be one-time only")
	}
	if _, leaked := out["credential"].(map[string]any)["secret_hash"]; leaked {
		t.Error("secret_hash exposed via GET")
	}

	// List.
	status, out = do(t, "GET", ts.URL+"/v1/domains/"+did+"/credentials", testToken, nil)
	if status != http.StatusOK || len(out["credentials"].([]any)) != 1 {
		t.Fatalf("list credentials %d %v", status, out)
	}

	// Suspend.
	status, out = do(t, "PATCH", ts.URL+"/v1/credentials/"+cid, testToken, map[string]any{"status": "suspended"})
	if status != http.StatusOK || out["credential"].(map[string]any)["status"] != "suspended" {
		t.Fatalf("suspend %d %v", status, out)
	}

	// Invalid status → 400.
	status, _ = do(t, "PATCH", ts.URL+"/v1/credentials/"+cid, testToken, map[string]any{"status": "bogus"})
	if status != http.StatusBadRequest {
		t.Errorf("bad status = %d, want 400", status)
	}

	// Update restrictions.
	status, out = do(t, "PATCH", ts.URL+"/v1/credentials/"+cid, testToken, map[string]any{
		"restrictions": map[string]any{"max_messages_per_hour": 200},
	})
	if status != http.StatusOK {
		t.Fatalf("patch restrictions %d", status)
	}
	restr := out["credential"].(map[string]any)["restrictions"].(map[string]any)
	if restr["max_messages_per_hour"].(float64) != 200 {
		t.Errorf("restrictions not updated: %v", restr)
	}

	// Stats scaffold returns zeros.
	status, out = do(t, "GET", ts.URL+"/v1/credentials/"+cid+"/stats?window=24h", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("stats %d", status)
	}
	if out["stats"].(map[string]any)["submitted"].(float64) != 0 {
		t.Errorf("expected zero stats: %v", out["stats"])
	}

	// Delete.
	status, _ = do(t, "DELETE", ts.URL+"/v1/credentials/"+cid, testToken, nil)
	if status != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", status)
	}
	status, _ = do(t, "GET", ts.URL+"/v1/credentials/"+cid, testToken, nil)
	if status != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", status)
	}
}
