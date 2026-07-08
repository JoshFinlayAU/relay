package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// do is a small authenticated request helper. It fully reads and closes the
// response body, returning the status code and decoded JSON.
func do(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var out map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return resp.StatusCode, out
}

func TestAuthRequired(t *testing.T) {
	ts := newTestServer(t)
	status, out := do(t, "GET", ts.URL+"/v1/domains", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if out["error"].(map[string]any)["code"] != "unauthorized" {
		t.Errorf("error code = %v", out["error"])
	}

	status, _ = do(t, "GET", ts.URL+"/v1/domains", "wrong-token", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", status)
	}
}

func TestCreateDomainFlow(t *testing.T) {
	ts := newTestServer(t)

	status, out := do(t, "POST", ts.URL+"/v1/domains", testToken, map[string]any{"name": "Example.COM", "receiving": false})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%v)", status, out)
	}
	dom := out["domain"].(map[string]any)
	if dom["name"] != "example.com" {
		t.Errorf("name not normalized: %v", dom["name"])
	}
	if dom["status"] != "pending" {
		t.Errorf("status = %v, want pending", dom["status"])
	}
	if dom["bounce_subdomain"] != "bounce.example.com" {
		t.Errorf("bounce subdomain = %v", dom["bounce_subdomain"])
	}
	recs := out["dns"].([]any)
	if len(recs) != 6 {
		t.Fatalf("dns records = %d, want 6", len(recs))
	}
	// DKIM record must carry a real public key.
	foundDKIM := false
	for _, r := range recs {
		rm := r.(map[string]any)
		if rm["purpose"] == "dkim" {
			foundDKIM = true
			if !bytesContains(rm["value"].(string), "v=DKIM1; k=rsa; p=MII") {
				t.Errorf("dkim value looks wrong: %v", rm["value"])
			}
		}
	}
	if !foundDKIM {
		t.Error("no dkim record in response")
	}

	id := dom["id"].(string)

	// Duplicate → 409.
	status, _ = do(t, "POST", ts.URL+"/v1/domains", testToken, map[string]any{"name": "example.com"})
	if status != http.StatusConflict {
		t.Errorf("duplicate status = %d, want 409", status)
	}

	// Invalid domain → 400.
	status, _ = do(t, "POST", ts.URL+"/v1/domains", testToken, map[string]any{"name": "not a domain"})
	if status != http.StatusBadRequest {
		t.Errorf("invalid status = %d, want 400", status)
	}

	// List includes it.
	_, out = do(t, "GET", ts.URL+"/v1/domains", testToken, nil)
	if len(out["domains"].([]any)) != 1 {
		t.Errorf("list len = %d, want 1", len(out["domains"].([]any)))
	}

	// Get by id.
	status, _ = do(t, "GET", ts.URL+"/v1/domains/"+id, testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get status = %d", status)
	}

	// DNS instructions endpoint.
	status, out = do(t, "GET", ts.URL+"/v1/domains/"+id+"/dns", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("dns status = %d", status)
	}
	if _, ok := out["operator_note"]; !ok {
		t.Error("dns response missing operator_note")
	}

	// PATCH receiving → adds inbound MX to instructions.
	status, out = do(t, "PATCH", ts.URL+"/v1/domains/"+id, testToken, map[string]any{"receiving": true})
	if status != http.StatusOK || out["domain"].(map[string]any)["receiving"] != true {
		t.Fatalf("patch receiving failed: %d %v", status, out)
	}
	_, out = do(t, "GET", ts.URL+"/v1/domains/"+id+"/dns", testToken, nil)
	if len(out["instructions"].([]any)) != 7 {
		t.Errorf("instructions after receiving = %d, want 7", len(out["instructions"].([]any)))
	}

	// PATCH pause sending.
	status, out = do(t, "PATCH", ts.URL+"/v1/domains/"+id, testToken, map[string]any{"sending_paused": true})
	if status != http.StatusOK || out["domain"].(map[string]any)["sending_paused"] != true {
		t.Errorf("pause failed: %d %v", status, out)
	}

	// Delete.
	status, _ = do(t, "DELETE", ts.URL+"/v1/domains/"+id, testToken, nil)
	if status != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", status)
	}
	status, _ = do(t, "GET", ts.URL+"/v1/domains/"+id, testToken, nil)
	if status != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", status)
	}
}

func TestGetUnknownDomain(t *testing.T) {
	ts := newTestServer(t)
	status, _ := do(t, "GET", ts.URL+"/v1/domains/00000000-0000-0000-0000-000000000000", testToken, nil)
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	status, _ = do(t, "GET", ts.URL+"/v1/domains/not-a-uuid", testToken, nil)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func bytesContains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func TestInjectionInputsRejected(t *testing.T) {
	ts := newTestServer(t)
	// Domain names with CRLF / injection payloads must be rejected (400).
	for _, bad := range []string{
		"evil.com\r\nBcc: x@y.com", "ex ample.com", "<script>.com", "a.com;rm -rf", "..",
	} {
		status, _ := do(t, "POST", ts.URL+"/v1/domains", testToken, map[string]any{"name": bad})
		if status != http.StatusBadRequest {
			t.Errorf("domain %q accepted (status %d), want 400", bad, status)
		}
	}
	// Valid domain, then injection in credential local part.
	did := createDomainForTest(t, ts.URL, testToken, "inj.example")
	for _, bad := range []string{"a b", "x\r\ny", "a@b", "<x>"} {
		status, _ := do(t, "POST", ts.URL+"/v1/domains/"+did+"/credentials", testToken, map[string]any{"name": bad})
		if status != http.StatusBadRequest {
			t.Errorf("credential local part %q accepted (status %d), want 400", bad, status)
		}
	}
}
