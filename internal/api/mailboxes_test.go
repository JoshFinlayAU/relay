package api

import (
	"net/http"
	"testing"
)

func TestMailboxLifecycle(t *testing.T) {
	ts := newTestServer(t)
	did := createDomainForTest(t, ts.URL, testToken, "mbx.example")

	// Create - secret returned once.
	status, out := do(t, "POST", ts.URL+"/v1/domains/"+did+"/mailboxes", testToken, map[string]any{
		"local_part": "support", "webhook_url": "https://example.test/hook",
	})
	if status != http.StatusCreated {
		t.Fatalf("create mailbox = %d (%v)", status, out)
	}
	if out["secret"] == nil || out["secret"].(string) == "" {
		t.Error("webhook secret not returned on create")
	}
	mb := out["mailbox"].(map[string]any)
	if mb["local_part"] != "support" {
		t.Errorf("local_part = %v", mb["local_part"])
	}
	mid := mb["id"].(string)

	// Non-http webhook URL rejected.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/mailboxes", testToken, map[string]any{
		"local_part": "x", "webhook_url": "ftp://nope",
	})
	if status != http.StatusBadRequest {
		t.Errorf("bad webhook url = %d, want 400", status)
	}

	// Catch-all is allowed.
	status, _ = do(t, "POST", ts.URL+"/v1/domains/"+did+"/mailboxes", testToken, map[string]any{
		"local_part": "*", "webhook_url": "https://example.test/catchall",
	})
	if status != http.StatusCreated {
		t.Errorf("catch-all create = %d, want 201", status)
	}

	// List shows both; never leaks the secret.
	status, out = do(t, "GET", ts.URL+"/v1/domains/"+did+"/mailboxes", testToken, nil)
	if status != http.StatusOK || len(out["mailboxes"].([]any)) != 2 {
		t.Fatalf("list = %d %v", status, out)
	}
	for _, m := range out["mailboxes"].([]any) {
		if _, leaked := m.(map[string]any)["webhook_secret_enc"]; leaked {
			t.Error("mailbox secret leaked in list")
		}
	}

	// Delete.
	status, _ = do(t, "DELETE", ts.URL+"/v1/mailboxes/"+mid, testToken, nil)
	if status != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", status)
	}

	// Webhook delivery log endpoint responds (empty).
	status, _ = do(t, "GET", ts.URL+"/v1/domains/"+did+"/webhook-deliveries", testToken, nil)
	if status != http.StatusOK {
		t.Errorf("webhook-deliveries = %d", status)
	}
}
