package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"time"

	"github.com/caddyserver/certmagic"
)

// ACMEConfig configures Let's Encrypt (or a compatible ACME CA) issuance.
type ACMEConfig struct {
	Hostname   string
	Email      string
	StorageDir string
	Staging    bool
	CA         string // optional directory URL override (e.g. Pebble)
}

// Manager wraps certmagic: it manages the hostname's certificate (issuance +
// renewal) and exposes TLS configs for HTTPS and SMTP plus the ACME HTTP-01
// challenge handler.
type Manager struct {
	magic  *certmagic.Config
	issuer *certmagic.ACMEIssuer
}

// hardenedCiphers is the TLS 1.2 cipher allow-list (TLS 1.3 suites are fixed by
// the stdlib and always enabled).
var hardenedCiphers = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
}

// NewManager configures certmagic and synchronously obtains/loads the cert for
// the hostname. The ACME HTTP-01 challenge server must be listening on :80
// before (or concurrently with) this call - see ChallengeHandler.
func NewManager(ctx context.Context, cfg ACMEConfig) (*Manager, error) {
	if cfg.Hostname == "" {
		return nil, fmt.Errorf("tls: hostname required")
	}
	certmagic.Default.Storage = &certmagic.FileStorage{Path: cfg.StorageDir}

	acme := certmagic.DefaultACME
	acme.Agreed = true
	acme.Email = cfg.Email
	acme.DisableTLSALPNChallenge = true // we only run HTTP-01 (SMTP ports aren't ALPN)
	switch {
	case cfg.CA != "":
		acme.CA = cfg.CA
	case cfg.Staging:
		acme.CA = certmagic.LetsEncryptStagingCA
	default:
		acme.CA = certmagic.LetsEncryptProductionCA
	}

	magic := certmagic.NewDefault()
	issuer := certmagic.NewACMEIssuer(magic, acme)
	magic.Issuers = []certmagic.Issuer{issuer}

	return &Manager{magic: magic, issuer: issuer}, nil
}

// Obtain manages (issues/renews as needed) the hostname's certificate.
func (m *Manager) Obtain(ctx context.Context, hostname string) error {
	return m.magic.ManageSync(ctx, []string{hostname})
}

// ChallengeHandler wraps next so ACME HTTP-01 challenge requests are answered;
// everything else falls through to next (typically an HTTPS redirect).
func (m *Manager) ChallengeHandler(next http.Handler) http.Handler {
	return m.issuer.HTTPChallengeHandler(next)
}

// HTTPSTLSConfig returns the TLS config for the HTTPS server (with ALPN).
func (m *Manager) HTTPSTLSConfig() *tls.Config {
	c := m.magic.TLSConfig()
	c.MinVersion = tls.VersionTLS12
	c.CipherSuites = hardenedCiphers
	c.NextProtos = append([]string{"h2", "http/1.1"}, c.NextProtos...)
	return c
}

// LeafNotAfter returns the managed leaf certificate's expiry for the hostname.
func (m *Manager) LeafNotAfter(hostname string) (time.Time, bool) {
	cert, err := m.magic.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: hostname})
	if err != nil || cert == nil || len(cert.Certificate) == 0 {
		return time.Time{}, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return time.Time{}, false
	}
	return leaf.NotAfter, true
}

// SMTPTLSConfig returns a TLS config for the SMTP listeners that serves the
// managed cert (no HTTP ALPN protocols).
func (m *Manager) SMTPTLSConfig() *tls.Config {
	base := m.magic.TLSConfig()
	return &tls.Config{
		GetCertificate: base.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites:   hardenedCiphers,
	}
}
