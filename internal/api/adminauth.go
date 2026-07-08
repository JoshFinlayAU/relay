package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/auth"
	"relay/internal/store"
)

// SessionTTL is how long a WebUI session stays valid.
const SessionTTL = 24 * time.Hour

// loginLimiter throttles admin login brute force per IP+username.
var loginLimiter = auth.NewFailLimiter(10, time.Minute)

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SeedAdminUser creates the bootstrap admin account if none exist yet.
func SeedAdminUser(ctx context.Context, st *store.Store, username, password string) error {
	if username == "" || password == "" {
		return nil
	}
	n, err := st.CountAdminUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := auth.HashSecret(password)
	if err != nil {
		return err
	}
	_, err = st.CreateAdminUser(ctx, store.CreateAdminUserParams{Username: username, PasswordHash: hash})
	return err
}

// --- login / logout ---

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if loginLimiter.Blocked(ip) || loginLimiter.Blocked(req.Username) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
		return
	}
	_ = s.Store.DeleteExpiredSessions(r.Context())

	fail := func() {
		loginLimiter.RecordFailure(ip)
		loginLimiter.RecordFailure(req.Username)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
	}

	u, err := s.Store.GetAdminUserByUsername(r.Context(), req.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		fail()
		return
	}
	if err != nil {
		errInternal(w, s.Log, "get admin user", err)
		return
	}
	if u.Disabled {
		fail()
		return
	}
	ok, err := auth.VerifySecret(req.Password, u.PasswordHash)
	if err != nil || !ok {
		fail()
		return
	}

	token, err := auth.GenerateSecret()
	if err != nil {
		errInternal(w, s.Log, "gen session", err)
		return
	}
	expires := time.Now().Add(SessionTTL)
	if _, err := s.Store.CreateSession(r.Context(), store.CreateSessionParams{
		UserID:    u.ID,
		TokenHash: sha256hex(token),
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		errInternal(w, s.Log, "create session", err)
		return
	}
	loginLimiter.Reset(ip)
	loginLimiter.Reset(req.Username)
	_ = s.Store.TouchAdminLogin(r.Context(), u.ID)

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expires.UTC(),
		"user":       map[string]any{"id": u.ID.String(), "username": u.Username},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok := bearerToken(r); tok != "" {
		_ = s.Store.DeleteSessionByTokenHash(r.Context(), sha256hex(tok))
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- admin user management ---

type adminUserDTO struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Disabled  bool       `json:"disabled"`
	CreatedAt *time.Time `json:"created_at"`
	LastLogin *time.Time `json:"last_login"`
}

func (s *Server) handleListAdminUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListAdminUsers(r.Context())
	if err != nil {
		errInternal(w, s.Log, "list admin users", err)
		return
	}
	out := make([]adminUserDTO, 0, len(rows))
	for _, u := range rows {
		out = append(out, adminUserDTO{
			ID: u.ID.String(), Username: u.Username, Disabled: u.Disabled,
			CreatedAt: tsPtr(u.CreatedAt), LastLogin: tsPtr(u.LastLogin),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createAdminReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleCreateAdminUser(w http.ResponseWriter, r *http.Request) {
	var req createAdminReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if len(req.Username) < 3 {
		errBadRequest(w, "invalid_username", "username must be at least 3 characters")
		return
	}
	if len(req.Password) < 8 {
		errBadRequest(w, "weak_password", "password must be at least 8 characters")
		return
	}
	if _, err := s.Store.GetAdminUserByUsername(r.Context(), req.Username); err == nil {
		errConflict(w, "user_exists", "username already taken")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		errInternal(w, s.Log, "check user", err)
		return
	}
	hash, err := auth.HashSecret(req.Password)
	if err != nil {
		errInternal(w, s.Log, "hash password", err)
		return
	}
	u, err := s.Store.CreateAdminUser(r.Context(), store.CreateAdminUserParams{Username: req.Username, PasswordHash: hash})
	if err != nil {
		errInternal(w, s.Log, "create admin user", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": adminUserDTO{ID: u.ID.String(), Username: u.Username, CreatedAt: tsPtr(u.CreatedAt)}})
}

type changePasswordReq struct {
	Password string `json:"password"`
}

func (s *Server) handleChangeAdminPassword(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	var req changePasswordReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	if len(req.Password) < 8 {
		errBadRequest(w, "weak_password", "password must be at least 8 characters")
		return
	}
	hash, err := auth.HashSecret(req.Password)
	if err != nil {
		errInternal(w, s.Log, "hash password", err)
		return
	}
	if err := s.Store.UpdateAdminPassword(r.Context(), store.UpdateAdminPasswordParams{ID: id, PasswordHash: hash}); err != nil {
		errInternal(w, s.Log, "update password", err)
		return
	}
	// Invalidate existing sessions for that user (force re-login).
	_ = s.Store.DeleteUserSessions(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAdminUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid_id", "id must be a UUID")
		return
	}
	// Refuse to delete the last remaining admin (lockout guard).
	if n, err := s.Store.CountAdminUsers(r.Context()); err == nil && n <= 1 {
		errBadRequest(w, "last_admin", "cannot delete the only admin user")
		return
	}
	if err := s.Store.DeleteAdminUser(r.Context(), id); err != nil {
		errInternal(w, s.Log, "delete admin user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
