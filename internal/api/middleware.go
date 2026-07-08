package api

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// requireAuth accepts either a static config bearer token (break-glass / API /
// scripts) or a valid WebUI session token. No token value is ever logged.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := bearerToken(r)
		if got == "" || !s.credentialValid(r, got) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="relay"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// credentialValid checks a static token first, then a session token.
func (s *Server) credentialValid(r *http.Request, got string) bool {
	if s.tokenValid(got) {
		return true
	}
	sess, err := s.Store.GetSessionByTokenHash(r.Context(), sha256hex(got))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.Log.Error("session lookup", "err", err)
		}
		return false
	}
	if sess.Disabled || !sess.ExpiresAt.Valid || time.Now().After(sess.ExpiresAt.Time) {
		return false
	}
	_ = s.Store.TouchSession(r.Context(), sess.ID)
	return true
}

// clientIP returns the connecting IP (RemoteAddr host); no proxy headers are
// trusted (relayd faces its public IP directly).
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

func (s *Server) tokenValid(got string) bool {
	ok := false
	for _, t := range s.Tokens {
		if subtle.ConstantTimeCompare([]byte(got), []byte(t)) == 1 {
			ok = true
		}
	}
	return ok
}
