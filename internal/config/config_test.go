package config

import (
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestDeriveFromHostname(t *testing.T) {
	c := Config{Hostname: "mail.example.com"}
	c.derive()
	if c.SPFInclude != "spf.mail.example.com" {
		t.Errorf("SPFInclude = %q, want spf.mail.example.com", c.SPFInclude)
	}
	if c.DMARCRua != "mailto:dmarc@example.com" {
		t.Errorf("DMARCRua = %q, want mailto:dmarc@example.com", c.DMARCRua)
	}

	// Explicit values must be preserved (not overwritten).
	c2 := Config{Hostname: "mail.example.com", SPFInclude: "custom.spf.host", DMARCRua: "mailto:x@y.z", SendingIPv4: "203.0.113.5"}
	c2.derive()
	if c2.SPFInclude != "custom.spf.host" || c2.DMARCRua != "mailto:x@y.z" || c2.SendingIPv4 != "203.0.113.5" {
		t.Errorf("derive overwrote explicit values: %+v", c2)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"ok", func(*Config) {}, false},
		{"no hostname", func(c *Config) { c.Hostname = "" }, true},
		{"no db", func(c *Config) { c.DatabaseURL = "" }, true},
		{"no http", func(c *Config) { c.HTTPAddr = "" }, true},
		{"bad maxconns", func(c *Config) { c.MaxConns = 0 }, true},
		{"bad loglevel", func(c *Config) { c.LogLevel = "loud" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.mutate(&c)
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyEnv(t *testing.T) {
	t.Setenv("RELAY_HOSTNAME", "mail.example.com")
	t.Setenv("RELAY_MAX_CONNS", "42")
	t.Setenv("RELAY_AUTO_MIGRATE", "false")
	t.Setenv("RELAY_ADMIN_TOKENS", "a, b ,c")
	t.Setenv("RELAY_DNS_REVERIFY_INTERVAL", "2h")

	c := Default()
	c.applyEnv()

	if c.Hostname != "mail.example.com" {
		t.Errorf("hostname=%q", c.Hostname)
	}
	if c.MaxConns != 42 {
		t.Errorf("maxconns=%d", c.MaxConns)
	}
	if c.AutoMigrate {
		t.Errorf("auto_migrate should be false")
	}
	if len(c.AdminTokens) != 3 || c.AdminTokens[2] != "c" {
		t.Errorf("admin tokens=%v", c.AdminTokens)
	}
	if c.DNSReverifyInterval != 2*time.Hour {
		t.Errorf("reverify=%v", c.DNSReverifyInterval)
	}
}
