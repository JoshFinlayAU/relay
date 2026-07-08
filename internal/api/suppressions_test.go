package api

import (
	"net/http"
	"testing"
)

func TestSuppressionCRUD(t *testing.T) {
	ts := newTestServer(t)
	did := createDomainForTest(t, ts.URL, testToken, "supp.example")

	// Empty initially.
	status, out := do(t, "GET", ts.URL+"/v1/domains/"+did+"/suppressions", testToken, nil)
	if status != http.StatusOK || len(out["suppressions"].([]any)) != 0 {
		t.Fatalf("initial list %d %v", status, out)
	}

	// Add.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/suppressions", testToken, map[string]any{"address": "Bad@Example.net", "reason": "manual test"})
	if status != http.StatusCreated {
		t.Fatalf("add suppression = %d", status)
	}
	// Invalid address rejected.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/suppressions", testToken, map[string]any{"address": "notanemail"})
	if status != http.StatusBadRequest {
		t.Errorf("invalid address = %d, want 400", status)
	}

	// Listed (lower-cased).
	status, out = do(t, "GET", ts.URL+"/v1/domains/"+did+"/suppressions", testToken, nil)
	sup := out["suppressions"].([]any)
	if status != http.StatusOK || len(sup) != 1 {
		t.Fatalf("list after add = %d %v", status, out)
	}
	if sup[0].(map[string]any)["address"] != "bad@example.net" {
		t.Errorf("address = %v", sup[0].(map[string]any)["address"])
	}

	// Remove (override).
	status, _ = do(t, "DELETE", ts.URL+"/v1/domains/"+did+"/suppressions?address=bad@example.net", testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", status)
	}
	_, out = do(t, "GET", ts.URL+"/v1/domains/"+did+"/suppressions", testToken, nil)
	if len(out["suppressions"].([]any)) != 0 {
		t.Errorf("should be empty after removal: %v", out)
	}
}
