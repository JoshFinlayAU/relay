package creds

import "testing"

func TestFromAllowed(t *testing.T) {
	tests := []struct {
		name    string
		patts   []string
		addr    string
		allowed bool
	}{
		{"empty allows all", nil, "anyone@example.com", true},
		{"exact match", []string{"orders@example.com"}, "orders@example.com", true},
		{"exact mismatch", []string{"orders@example.com"}, "sales@example.com", false},
		{"case insensitive", []string{"Orders@Example.com"}, "orders@example.com", true},
		{"local wildcard", []string{"*@example.com"}, "anything@example.com", true},
		{"local wildcard wrong domain", []string{"*@example.com"}, "x@evil.com", false},
		{"star all", []string{"*"}, "x@y.com", true},
		{"suffix trick blocked", []string{"*@example.com"}, "x@notexample.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Restrictions{AllowedFrom: tt.patts}
			if got := r.FromAllowed(tt.addr); got != tt.allowed {
				t.Errorf("FromAllowed(%q) = %v, want %v", tt.addr, got, tt.allowed)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	if err := (Restrictions{MaxRecipients: -1}).Validate(); err == nil {
		t.Error("expected error for negative limit")
	}
	if err := (Restrictions{AllowedFrom: []string{" "}}).Validate(); err == nil {
		t.Error("expected error for blank pattern")
	}
	if err := (Restrictions{MaxMessagesPerHour: 100, AllowedFrom: []string{"*@x.com"}}).Validate(); err != nil {
		t.Errorf("valid restrictions rejected: %v", err)
	}
}

func TestParseJSONRoundTrip(t *testing.T) {
	r := Restrictions{AllowedFrom: []string{"*@x.com"}, MaxMessagesPerHour: 50, MaxMessageSize: 1024}
	b, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxMessagesPerHour != 50 || got.MaxMessageSize != 1024 || len(got.AllowedFrom) != 1 {
		t.Errorf("round trip mismatch: %+v", got)
	}
	// Empty input → zero value, no error.
	if _, err := Parse(nil); err != nil {
		t.Errorf("parse nil: %v", err)
	}
}
