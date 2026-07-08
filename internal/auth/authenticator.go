package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"relay/internal/store"
)

// Sentinel errors. AUTH failures deliberately do not distinguish "no such user"
// from "wrong secret" to avoid user enumeration.
var (
	ErrAuthFailed  = errors.New("authentication failed")
	ErrLocked      = errors.New("credential locked")
	ErrRateLimited = errors.New("too many attempts")
	ErrNotActive   = errors.New("credential not active")
)

// Config tunes lockout and rate-limiting behaviour.
type Config struct {
	MaxFailures  int           // lock credential after this many consecutive failures
	LockDuration time.Duration // how long a credential stays locked
	RateMax      int           // failures per window per IP / per username before throttling
	RateWindow   time.Duration
}

// DefaultConfig returns sane defaults.
func DefaultConfig() Config {
	return Config{
		MaxFailures:  5,
		LockDuration: 15 * time.Minute,
		RateMax:      10,
		RateWindow:   1 * time.Minute,
	}
}

// Authenticator verifies credentials with lockout + rate limiting.
type Authenticator struct {
	store *store.Store
	cfg   Config
	ipLim *FailLimiter
	usLim *FailLimiter
	now   func() time.Time
}

// NewAuthenticator builds an Authenticator.
func NewAuthenticator(st *store.Store, cfg Config) *Authenticator {
	return &Authenticator{
		store: st,
		cfg:   cfg,
		ipLim: NewFailLimiter(cfg.RateMax, cfg.RateWindow),
		usLim: NewFailLimiter(cfg.RateMax, cfg.RateWindow),
		now:   time.Now,
	}
}

// Authenticate verifies username+secret. ip is used for per-IP throttling and
// may be empty. On success it returns the credential and resets counters.
func (a *Authenticator) Authenticate(ctx context.Context, username, secret, ip string) (*store.Credential, error) {
	// Rate-limit gate first (cheap, protects the DB and argon2 from brute force).
	if a.ipLim.Blocked(ip) || a.usLim.Blocked(username) {
		return nil, ErrRateLimited
	}

	cred, err := a.store.GetCredentialByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		a.recordFailure(ip, username)
		return nil, ErrAuthFailed
	}
	if err != nil {
		return nil, err
	}

	if cred.Status != "active" {
		a.recordFailure(ip, username)
		return nil, ErrNotActive
	}
	if cred.LockedUntil.Valid && a.now().Before(cred.LockedUntil.Time) {
		a.recordFailure(ip, username)
		return nil, ErrLocked
	}

	ok, err := VerifySecret(secret, cred.SecretHash)
	if err != nil || !ok {
		a.onBadSecret(ctx, &cred)
		a.recordFailure(ip, username)
		return nil, ErrAuthFailed
	}

	// Success: clear counters + failed-auth state.
	a.ipLim.Reset(ip)
	a.usLim.Reset(username)
	_ = a.store.TouchCredentialLastUsed(ctx, cred.ID)
	return &cred, nil
}

// onBadSecret increments the persistent failure count and locks the credential
// (with an event) once the threshold is reached.
func (a *Authenticator) onBadSecret(ctx context.Context, cred *store.Credential) {
	count, err := a.store.RecordFailedAuth(ctx, cred.ID)
	if err != nil {
		return
	}
	if int(count) >= a.cfg.MaxFailures {
		lockUntil := pgtype.Timestamptz{Time: a.now().Add(a.cfg.LockDuration), Valid: true}
		_ = a.store.SetCredentialLock(ctx, store.SetCredentialLockParams{ID: cred.ID, LockedUntil: lockUntil})
		_ = a.store.EmitEvent(ctx, cred.DomainID, "credential.locked", map[string]any{
			"credential_id": cred.ID.String(),
			"username":      cred.Username,
			"failures":      count,
		})
	}
}

func (a *Authenticator) recordFailure(ip, username string) {
	a.ipLim.RecordFailure(ip)
	a.usLim.RecordFailure(username)
}
