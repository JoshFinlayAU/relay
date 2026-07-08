package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"relay/internal/store"
)

// SettingKey is the app_settings row that holds the message-retention policy.
const SettingKey = "retention"

// Modes.
const (
	ModeAge   = "age"   // keep messages newer than Days
	ModeCount = "count" // keep the newest MaxMessages
)

// Policy is the runtime-editable message-retention policy (set from the WebUI).
// It governs how long message + delivery history is kept; stored bodies are
// still reaped on their own shorter TTL and never outlive their message row.
type Policy struct {
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`         // "age" | "count"
	Days        int    `json:"days"`         // age mode: keep messages newer than this many days
	MaxMessages int    `json:"max_messages"` // count mode: keep this many newest messages
}

// Validate checks a policy submitted from the API.
func (p Policy) Validate() error {
	switch p.Mode {
	case ModeAge:
		if p.Days < 1 || p.Days > 3650 {
			return fmt.Errorf("days must be between 1 and 3650")
		}
	case ModeCount:
		if p.MaxMessages < 1 {
			return fmt.Errorf("max_messages must be at least 1")
		}
	default:
		return fmt.Errorf("mode must be %q or %q", ModeAge, ModeCount)
	}
	return nil
}

// LoadPolicy returns the stored retention policy and whether one is set.
func LoadPolicy(ctx context.Context, st *store.Store) (Policy, bool, error) {
	raw, err := st.GetSetting(ctx, SettingKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, false, err
	}
	var p Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return Policy{}, false, fmt.Errorf("decode retention policy: %w", err)
	}
	return p, true, nil
}

// SavePolicy persists a validated policy.
func SavePolicy(ctx context.Context, st *store.Store, p Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return st.UpsertSetting(ctx, store.UpsertSettingParams{Key: SettingKey, Value: b})
}
