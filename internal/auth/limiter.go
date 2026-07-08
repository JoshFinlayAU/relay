package auth

import (
	"sync"
	"time"
)

// failLimiter is a fixed-window failure counter keyed by IP or username. It is
// used to throttle/tarpit brute-force AUTH attempts.
type FailLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	buckets map[string]*failBucket
	now     func() time.Time
}

type failBucket struct {
	count   int
	resetAt time.Time
}

func NewFailLimiter(max int, window time.Duration) *FailLimiter {
	return &FailLimiter{
		max:     max,
		window:  window,
		buckets: make(map[string]*failBucket),
		now:     time.Now,
	}
}

// recordFailure increments the failure count for key within the current window.
func (l *FailLimiter) RecordFailure(key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil || now.After(b.resetAt) {
		b = &failBucket{resetAt: now.Add(l.window)}
		l.buckets[key] = b
	}
	b.count++
}

// blocked reports whether key has exceeded the failure threshold this window.
func (l *FailLimiter) Blocked(key string) bool {
	if key == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil || l.now().After(b.resetAt) {
		return false
	}
	return b.count >= l.max
}

// reset clears a key's counter (on successful auth).
func (l *FailLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
