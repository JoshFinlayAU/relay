// Package creds defines the per-credential restriction model shared by the API
// (create/edit) and the submission listener (enforcement).
package creds

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Restrictions are optional per-credential limits. Zero values mean "no limit".
type Restrictions struct {
	// AllowedFrom is a list of From-address patterns the credential may use.
	// Empty means "any address within the credential's domain scope".
	// Patterns: exact ("orders@example.com"), local wildcard ("*@example.com"),
	// or "*" (any). Matching is case-insensitive.
	AllowedFrom        []string `json:"allowed_from,omitempty"`
	MaxMessagesPerHour int      `json:"max_messages_per_hour,omitempty"`
	MaxRecipients      int      `json:"max_recipients,omitempty"`
	MaxMessageSize     int64    `json:"max_message_size,omitempty"`
	// SuppressionOverride lets this credential send to suppressed (hard-bounced)
	// addresses anyway - for genuinely transactional resends.
	SuppressionOverride bool `json:"suppression_override,omitempty"`
}

// Parse decodes restrictions from stored JSONB.
func Parse(raw []byte) (Restrictions, error) {
	var r Restrictions
	if len(raw) == 0 {
		return r, nil
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("parse restrictions: %w", err)
	}
	return r, nil
}

// JSON encodes restrictions for storage.
func (r Restrictions) JSON() ([]byte, error) { return json.Marshal(r) }

// Validate checks restriction values are sane.
func (r Restrictions) Validate() error {
	if r.MaxMessagesPerHour < 0 || r.MaxRecipients < 0 || r.MaxMessageSize < 0 {
		return fmt.Errorf("restriction limits must be non-negative")
	}
	for _, p := range r.AllowedFrom {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("allowed_from patterns must be non-empty")
		}
	}
	return nil
}

// FromAllowed reports whether a From address is permitted by AllowedFrom.
// An empty AllowedFrom permits anything (domain-scope enforcement happens
// separately).
func (r Restrictions) FromAllowed(addr string) bool {
	if len(r.AllowedFrom) == 0 {
		return true
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	for _, pat := range r.AllowedFrom {
		pat = strings.ToLower(strings.TrimSpace(pat))
		if pat == "*" || pat == addr {
			return true
		}
		if strings.HasPrefix(pat, "*@") {
			if dom := pat[1:]; strings.HasSuffix(addr, dom) && strings.Contains(addr, "@") {
				// ensure the @domain matches exactly (not a suffix trick)
				at := strings.LastIndex(addr, "@")
				if addr[at:] == dom {
					return true
				}
			}
		}
	}
	return false
}
