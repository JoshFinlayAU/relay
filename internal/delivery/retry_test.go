package delivery

import (
	"testing"
	"time"
)

func TestNextDelay(t *testing.T) {
	cases := map[int]time.Duration{
		1:  time.Minute,
		2:  5 * time.Minute,
		3:  15 * time.Minute,
		4:  time.Hour,
		5:  4 * time.Hour,
		6:  8 * time.Hour,
		7:  8 * time.Hour,
		20: 8 * time.Hour,
	}
	for attempts, want := range cases {
		if got := NextDelay(attempts); got != want {
			t.Errorf("NextDelay(%d) = %v, want %v", attempts, got, want)
		}
	}
}

func TestRetryPolicyCustom(t *testing.T) {
	rp := RetryPolicy{Schedule: []time.Duration{2 * time.Second, 30 * time.Second}, MaxAge: time.Hour}
	if got := rp.NextDelay(1); got != 2*time.Second {
		t.Errorf("NextDelay(1) = %v, want 2s", got)
	}
	if got := rp.NextDelay(2); got != 30*time.Second {
		t.Errorf("NextDelay(2) = %v, want 30s", got)
	}
	// Beyond the schedule, the last entry repeats.
	if got := rp.NextDelay(5); got != 30*time.Second {
		t.Errorf("NextDelay(5) = %v, want 30s (last repeats)", got)
	}
	start := time.Unix(0, 0).UTC()
	if !rp.GiveUp(start, start.Add(2*time.Hour)) {
		t.Error("expected GiveUp past custom MaxAge")
	}
	if rp.GiveUp(start, start.Add(30*time.Minute)) {
		t.Error("did not expect GiveUp within custom MaxAge")
	}
	// Zero-value policy falls back to defaults.
	var zero RetryPolicy
	if got := zero.NextDelay(1); got != time.Minute {
		t.Errorf("zero policy NextDelay(1) = %v, want default 1m", got)
	}
}

func TestGiveUp(t *testing.T) {
	start := time.Now()
	if GiveUp(start, start.Add(71*time.Hour)) {
		t.Error("should not give up before 72h")
	}
	if !GiveUp(start, start.Add(73*time.Hour)) {
		t.Error("should give up after 72h")
	}
}
