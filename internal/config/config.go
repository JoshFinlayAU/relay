// Package config loads and validates relayd configuration from environment
// variables with an optional TOML overlay file (path in RELAY_CONFIG).
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"golang.org/x/net/publicsuffix"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// Server identity.
	Hostname string `toml:"hostname"` // EHLO name / cert subject, e.g. mail.as135559.net.au

	// Listeners.
	HTTPAddr       string `toml:"http_addr"`        // API + WebUI + ACME, e.g. ":443" (":8080" in dev)
	MetricsAddr    string `toml:"metrics_addr"`     // Prometheus, e.g. ":9090" ("" = share HTTP mux)
	SubmissionAddr string `toml:"submission_addr"`  // 587 STARTTLS submission
	SubmissionsTLS string `toml:"submissions_addr"` // 465 implicit TLS
	InboundAddr    string `toml:"inbound_addr"`     // 25 inbound/bounce

	// Database.
	DatabaseURL string `toml:"database_url"` // pgx connection string
	MaxConns    int32  `toml:"max_conns"`

	// Migrations.
	AutoMigrate bool `toml:"auto_migrate"` // run pending migrations on boot

	// Secrets.
	SecretKeyB64 string `toml:"secret_key"` // 32-byte base64 AES-256-GCM key for at-rest encryption

	// Auth.
	AdminTokens []string `toml:"admin_tokens"` // static bearer tokens (break-glass / API / scripts)
	// Bootstrap local admin account, created on boot if no admin users exist yet.
	AdminUser     string `toml:"admin_user"`
	AdminPassword string `toml:"admin_password"`

	// Storage.
	StorageDir string `toml:"storage_dir"` // content-addressed message bodies

	// Submission.
	MaxMessageBytes   int64 `toml:"max_message_bytes"`  // hard cap on submitted message size
	SubmissionEnabled bool  `toml:"submission_enabled"` // start 587/465 listeners
	InboundEnabled    bool  `toml:"inbound_enabled"`    // start port-25 listener (bounces; mailboxes in P6)

	// Delivery.
	DeliveryEnabled     bool   `toml:"delivery_enabled"`     // start outbound workers
	DeliveryConcurrency int    `toml:"delivery_concurrency"` // worker goroutines
	DeliveryPerDomain   int    `toml:"delivery_per_domain"`  // max concurrent conns per dest domain
	DeliverIPv6         bool   `toml:"deliver_ipv6"`         // send over IPv6 first, IPv4 fallback (default on)
	SMTPSink            string `toml:"smtp_sink"`            // host:port - route ALL delivery here (load testing)

	// Retry/backoff schedules (config-driven, not hardcoded). Empty ⇒ built-in defaults.
	DeliveryRetrySchedule []time.Duration `toml:"delivery_retry_schedule"` // 4xx backoff steps
	DeliveryMaxAge        time.Duration   `toml:"delivery_max_age"`        // give-up age → bounce
	WebhookRetrySchedule  []time.Duration `toml:"webhook_retry_schedule"`  // webhook retry steps
	WebhookMaxAge         time.Duration   `toml:"webhook_max_age"`         // dead-letter age

	// Retention (per-direction cleanup of stored bodies + metadata).
	RetentionEnabled        bool          `toml:"retention_enabled"`
	RetentionInterval       time.Duration `toml:"retention_interval"`        // how often the cleanup runs
	RetentionOutboundBodies time.Duration `toml:"retention_outbound_bodies"` // outbound raw bodies
	RetentionInboundBodies  time.Duration `toml:"retention_inbound_bodies"`  // inbound bodies (after webhook success)
	RetentionMetadata       time.Duration `toml:"retention_metadata"`        // message/attempt rows for stats

	// Inbound (port 25) connection limits.
	InboundMaxConns      int `toml:"inbound_max_conns"`        // total concurrent inbound conns (0 = unlimited)
	InboundMaxConnsPerIP int `toml:"inbound_max_conns_per_ip"` // per remote IP (0 = unlimited)

	// SMTP AUTH-allowed subnets on port 25 (CIDR).
	Port25AuthSubnets []string `toml:"port25_auth_subnets"`

	// Domain onboarding / DNS.
	SPFInclude   string   `toml:"spf_include"`   // include: target published in customer SPF
	DMARCRua     string   `toml:"dmarc_rua"`     // DMARC aggregate report address
	SendingIPv4  string   `toml:"sending_ipv4"`  // public IPv4 used for sending (SPF authority)
	SendingIPv6  string   `toml:"sending_ipv6"`  // public IPv6 used for sending
	DNSResolvers []string `toml:"dns_resolvers"` // bootstrap resolvers for NS discovery (host:port)

	// Timers.
	DNSReverifyInterval time.Duration `toml:"dns_reverify_interval"`

	// TLS / ACME (Let's Encrypt via certmagic). When enabled, the HTTP server
	// serves HTTPS on HTTPAddr (default :443), an ACME/redirect server runs on
	// ACMEHTTPAddr (:80), and the SMTP listeners share the managed cert.
	TLSEnabled   bool   `toml:"tls_enabled"`
	ACMEEmail    string `toml:"acme_email"`
	ACMEStaging  bool   `toml:"acme_staging"` // use LE staging (untrusted, high limits)
	ACMECA       string `toml:"acme_ca"`      // override directory URL (e.g. Pebble)
	ACMEHTTPAddr string `toml:"acme_http_addr"`

	// Logging.
	LogLevel string `toml:"log_level"` // debug|info|warn|error
}

// Default returns a Config populated with sane defaults before env/file overlay.
func Default() Config {
	return Config{
		Hostname:            "localhost",
		HTTPAddr:            ":8080",
		MetricsAddr:         "",
		SubmissionAddr:      ":587",
		SubmissionsTLS:      ":465",
		InboundAddr:         ":25",
		DatabaseURL:         "postgres://relay:relay_dev_pw@127.0.0.1:5432/relay?sslmode=disable",
		MaxConns:            10,
		AutoMigrate:         true,
		StorageDir:          "storage",
		DNSReverifyInterval: 6 * time.Hour,
		LogLevel:            "info",
		// SPFInclude / DMARCRua / SendingIPv4 / SendingIPv6 intentionally left
		// empty: derived from Hostname + detected public IPs in derive() unless
		// explicitly set in the config file or environment.
		DNSResolvers:        []string{"1.1.1.1:53", "8.8.8.8:53"},
		MaxMessageBytes:     26 << 20, // 26 MiB
		SubmissionEnabled:   true,
		InboundEnabled:      true,
		DeliveryEnabled:     true,
		DeliveryConcurrency: 8,
		DeliveryPerDomain:   2,
		DeliverIPv6:         true, // IPv6 first-class; falls back to IPv4 per attempt
		ACMEHTTPAddr:        ":80",

		// Retry schedules (CLAUDE.md defaults; overridable via config file).
		DeliveryRetrySchedule: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour, 8 * time.Hour},
		DeliveryMaxAge:        72 * time.Hour,
		WebhookRetrySchedule:  []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour},
		WebhookMaxAge:         24 * time.Hour,

		// Retention defaults (CLAUDE.md): outbound 7d, inbound success+7d, metadata 13mo.
		RetentionEnabled:        true,
		RetentionInterval:       6 * time.Hour,
		RetentionOutboundBodies: 7 * 24 * time.Hour,
		RetentionInboundBodies:  7 * 24 * time.Hour,
		RetentionMetadata:       13 * 30 * 24 * time.Hour,

		// Inbound connection limits (sane defaults for a single-IP MX).
		InboundMaxConns:      256,
		InboundMaxConnsPerIP: 10,
	}
}

// Load resolves configuration: defaults, then optional TOML file (RELAY_CONFIG),
// then environment variables (RELAY_* prefix), then validates.
func Load() (Config, error) {
	c := Default()

	if path := os.Getenv("RELAY_CONFIG"); path != "" {
		if _, err := toml.DecodeFile(path, &c); err != nil {
			return c, fmt.Errorf("decode config file %q: %w", path, err)
		}
	}

	c.applyEnv()
	c.derive()

	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// derive fills server-identity fields from the hostname and the host's public
// IPs when they weren't set explicitly, so a deployment only has to configure
// `hostname` to get correct SPF include / DMARC / sending IPs.
func (c *Config) derive() {
	if c.SPFInclude == "" && c.Hostname != "" {
		// The include target the operator publishes an SPF record at.
		c.SPFInclude = "spf." + c.Hostname
	}
	if c.DMARCRua == "" && c.Hostname != "" {
		c.DMARCRua = "mailto:dmarc@" + registrableDomain(c.Hostname)
	}
	if c.SendingIPv4 == "" {
		c.SendingIPv4 = detectPublicIP(false)
	}
	if c.SendingIPv6 == "" {
		c.SendingIPv6 = detectPublicIP(true)
	}
}

// registrableDomain returns the eTLD+1 of host (mail.example.com -> example.com),
// falling back to host itself when it isn't a normal public domain.
func registrableDomain(host string) string {
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	return host
}

// detectPublicIP returns the first globally-routable address bound to a local
// interface (the server's own public IP on a directly-addressed VM). Returns ""
// if none is found; operators can always override via config.
func detectPublicIP(v6 bool) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			continue
		}
		is6 := ip.To4() == nil
		if is6 == v6 {
			return ip.String()
		}
	}
	return ""
}

func (c *Config) applyEnv() {
	setStr := func(env string, dst *string) {
		if v, ok := os.LookupEnv(env); ok {
			*dst = v
		}
	}
	setStr("RELAY_HOSTNAME", &c.Hostname)
	setStr("RELAY_HTTP_ADDR", &c.HTTPAddr)
	setStr("RELAY_METRICS_ADDR", &c.MetricsAddr)
	setStr("RELAY_SUBMISSION_ADDR", &c.SubmissionAddr)
	setStr("RELAY_SUBMISSIONS_ADDR", &c.SubmissionsTLS)
	setStr("RELAY_INBOUND_ADDR", &c.InboundAddr)
	setStr("RELAY_DATABASE_URL", &c.DatabaseURL)
	setStr("RELAY_SECRET_KEY", &c.SecretKeyB64)
	setStr("RELAY_STORAGE_DIR", &c.StorageDir)
	setStr("RELAY_LOG_LEVEL", &c.LogLevel)
	setStr("RELAY_SPF_INCLUDE", &c.SPFInclude)
	setStr("RELAY_DMARC_RUA", &c.DMARCRua)
	setStr("RELAY_SENDING_IPV4", &c.SendingIPv4)
	setStr("RELAY_SENDING_IPV6", &c.SendingIPv6)
	if v, ok := os.LookupEnv("RELAY_DNS_RESOLVERS"); ok {
		c.DNSResolvers = splitAndTrim(v)
	}
	if v, ok := os.LookupEnv("RELAY_MAX_MESSAGE_BYTES"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.MaxMessageBytes = n
		}
	}
	if v, ok := os.LookupEnv("RELAY_SUBMISSION_ENABLED"); ok {
		c.SubmissionEnabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v, ok := os.LookupEnv("RELAY_INBOUND_ENABLED"); ok {
		c.InboundEnabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v, ok := os.LookupEnv("RELAY_DELIVER_IPV6"); ok {
		c.DeliverIPv6 = v == "1" || strings.EqualFold(v, "true")
	}
	if v, ok := os.LookupEnv("RELAY_TLS_ENABLED"); ok {
		c.TLSEnabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v, ok := os.LookupEnv("RELAY_ACME_STAGING"); ok {
		c.ACMEStaging = v == "1" || strings.EqualFold(v, "true")
	}
	setStr("RELAY_ACME_EMAIL", &c.ACMEEmail)
	setStr("RELAY_ACME_CA", &c.ACMECA)
	setStr("RELAY_ACME_HTTP_ADDR", &c.ACMEHTTPAddr)
	setStr("RELAY_SMTP_SINK", &c.SMTPSink)

	if v, ok := os.LookupEnv("RELAY_MAX_CONNS"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxConns = int32(n)
		}
	}
	if v, ok := os.LookupEnv("RELAY_AUTO_MIGRATE"); ok {
		c.AutoMigrate = v == "1" || strings.EqualFold(v, "true")
	}
	if v, ok := os.LookupEnv("RELAY_ADMIN_TOKENS"); ok {
		c.AdminTokens = splitAndTrim(v)
	}
	setStr("RELAY_ADMIN_USER", &c.AdminUser)
	setStr("RELAY_ADMIN_PASSWORD", &c.AdminPassword)
	if v, ok := os.LookupEnv("RELAY_PORT25_AUTH_SUBNETS"); ok {
		c.Port25AuthSubnets = splitAndTrim(v)
	}
	if v, ok := os.LookupEnv("RELAY_DNS_REVERIFY_INTERVAL"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			c.DNSReverifyInterval = d
		}
	}
	if v, ok := os.LookupEnv("RELAY_RETENTION_ENABLED"); ok {
		c.RetentionEnabled = v == "1" || strings.EqualFold(v, "true")
	}
	setDur := func(env string, dst *time.Duration) {
		if v, ok := os.LookupEnv(env); ok {
			if d, err := time.ParseDuration(v); err == nil {
				*dst = d
			}
		}
	}
	setDur("RELAY_RETENTION_INTERVAL", &c.RetentionInterval)
	setDur("RELAY_RETENTION_OUTBOUND_BODIES", &c.RetentionOutboundBodies)
	setDur("RELAY_RETENTION_INBOUND_BODIES", &c.RetentionInboundBodies)
	setDur("RELAY_RETENTION_METADATA", &c.RetentionMetadata)
	setInt := func(env string, dst *int) {
		if v, ok := os.LookupEnv(env); ok {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	setInt("RELAY_INBOUND_MAX_CONNS", &c.InboundMaxConns)
	setInt("RELAY_INBOUND_MAX_CONNS_PER_IP", &c.InboundMaxConnsPerIP)
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Validate checks that required fields are present and coherent.
func (c *Config) Validate() error {
	if c.Hostname == "" {
		return fmt.Errorf("hostname is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("database_url is required")
	}
	if c.HTTPAddr == "" {
		return fmt.Errorf("http_addr is required")
	}
	if c.MaxConns < 1 {
		return fmt.Errorf("max_conns must be >= 1")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log_level %q", c.LogLevel)
	}
	return nil
}
