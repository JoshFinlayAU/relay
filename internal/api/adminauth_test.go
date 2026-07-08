package api

import (
	"context"
	"net/http"
	"testing"

	"relay/internal/auth"
	"relay/internal/store"
)

func seedAdmin(t *testing.T, username, password string) {
	t.Helper()
	hash, err := auth.HashSecret(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.CreateAdminUser(context.Background(), store.CreateAdminUserParams{Username: username, PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
}

func TestLoginFlow(t *testing.T) {
	ts := newTestServer(t)
	seedAdmin(t, "tester", "password123")

	// Wrong password → 401.
	status, _ := do(t, "POST", ts.URL+"/v1/auth/login", "", map[string]any{"username": "tester", "password": "nope"})
	if status != http.StatusUnauthorized {
		t.Fatalf("bad password status = %d, want 401", status)
	}
	// Unknown user → 401 (no enumeration).
	status, _ = do(t, "POST", ts.URL+"/v1/auth/login", "", map[string]any{"username": "ghost", "password": "x"})
	if status != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d, want 401", status)
	}

	// Correct login → 200 + token.
	status, out := do(t, "POST", ts.URL+"/v1/auth/login", "", map[string]any{"username": "tester", "password": "password123"})
	if status != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (%v)", status, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("no session token returned")
	}

	// Session token authenticates a protected endpoint.
	status, _ = do(t, "GET", ts.URL+"/v1/domains", token, nil)
	if status != http.StatusOK {
		t.Fatalf("session-authed request = %d, want 200", status)
	}
	status, _ = do(t, "GET", ts.URL+"/v1/auth/verify", token, nil)
	if status != http.StatusOK {
		t.Errorf("verify with session = %d, want 200", status)
	}

	// Logout invalidates the session.
	status, _ = do(t, "POST", ts.URL+"/v1/auth/logout", token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", status)
	}
	status, _ = do(t, "GET", ts.URL+"/v1/domains", token, nil)
	if status != http.StatusUnauthorized {
		t.Errorf("request after logout = %d, want 401", status)
	}
}

func TestStaticTokenStillWorks(t *testing.T) {
	ts := newTestServer(t)
	// The break-glass static token authenticates without a session.
	status, _ := do(t, "GET", ts.URL+"/v1/domains", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("static token = %d, want 200", status)
	}
}

func TestAdminUserManagement(t *testing.T) {
	ts := newTestServer(t)
	seedAdmin(t, "root", "password123")

	// Create a second admin (auth with static token).
	status, out := do(t, "POST", ts.URL+"/v1/admin/users", testToken, map[string]any{"username": "second", "password": "password123"})
	if status != http.StatusCreated {
		t.Fatalf("create user = %d (%v)", status, out)
	}
	// Weak password rejected.
	status, _ = do(t, "POST", ts.URL+"/v1/admin/users", testToken, map[string]any{"username": "third", "password": "short"})
	if status != http.StatusBadRequest {
		t.Errorf("weak password = %d, want 400", status)
	}
	// Duplicate username.
	status, _ = do(t, "POST", ts.URL+"/v1/admin/users", testToken, map[string]any{"username": "second", "password": "password123"})
	if status != http.StatusConflict {
		t.Errorf("dup user = %d, want 409", status)
	}

	// List shows both.
	status, out = do(t, "GET", ts.URL+"/v1/admin/users", testToken, nil)
	if status != http.StatusOK || len(out["users"].([]any)) != 2 {
		t.Fatalf("list users = %d %v", status, out)
	}

	// New user can log in.
	_, lout := do(t, "POST", ts.URL+"/v1/auth/login", "", map[string]any{"username": "second", "password": "password123"})
	if lout["token"] == nil {
		t.Fatal("second user could not log in")
	}
	secondID := out["users"].([]any)[0].(map[string]any)["id"].(string)

	// Delete one, then the last-admin guard blocks deleting the final one.
	status, _ = do(t, "DELETE", ts.URL+"/v1/admin/users/"+secondID, testToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete user = %d, want 204", status)
	}
	// Find the remaining user's id.
	_, out = do(t, "GET", ts.URL+"/v1/admin/users", testToken, nil)
	remaining := out["users"].([]any)[0].(map[string]any)["id"].(string)
	status, _ = do(t, "DELETE", ts.URL+"/v1/admin/users/"+remaining, testToken, nil)
	if status != http.StatusBadRequest {
		t.Errorf("deleting last admin = %d, want 400 (lockout guard)", status)
	}
}
