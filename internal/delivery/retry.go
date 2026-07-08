package delivery

import "time"

// RetryPolicy is the configurable 4xx-deferral backoff. Defaults match CLAUDE.md
// (1m, 5m, 15m, 1h, 4h, 8h, then every 8h up to MaxAge, then bounce) but are
// overridable via config so schedules aren't hardcoded.
type RetryPolicy struct {
	Schedule []time.Duration // per-attempt backoff; the last entry repeats
	MaxAge   time.Duration   // total age after which a job bounces instead of deferring
}

// DefaultRetryPolicy returns the CLAUDE.md schedule.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Schedule: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
			1 * time.Hour,
			4 * time.Hour,
			8 * time.Hour,
		},
		MaxAge: 72 * time.Hour,
	}
}

// NextDelay returns the backoff before the next attempt given how many attempts
// have already been made (1 = first attempt just failed). The final schedule
// entry repeats for all further attempts.
func (rp RetryPolicy) NextDelay(attempts int) time.Duration {
	sched := rp.Schedule
	if len(sched) == 0 {
		sched = DefaultRetryPolicy().Schedule
	}
	idx := attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx < len(sched) {
		return sched[idx]
	}
	return sched[len(sched)-1]
}

// GiveUp reports whether a job queued at queuedAt should bounce rather than defer.
func (rp RetryPolicy) GiveUp(queuedAt, now time.Time) bool {
	max := rp.MaxAge
	if max <= 0 {
		max = DefaultRetryPolicy().MaxAge
	}
	return now.Sub(queuedAt) >= max
}

// MaxAge is the default give-up age (kept for reference/tests).
const MaxAge = 72 * time.Hour

// Package-level convenience wrappers over the default policy (used by tests and
// any caller that doesn't hold a Pool).
func NextDelay(attempts int) time.Duration { return DefaultRetryPolicy().NextDelay(attempts) }
func GiveUp(queuedAt, now time.Time) bool  { return DefaultRetryPolicy().GiveUp(queuedAt, now) }
