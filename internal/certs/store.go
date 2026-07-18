package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Entry is one operator-supplied certificate (decrypted key).
type Entry struct {
	DomainID *uuid.UUID // nil = server hostname cert
	CertPEM  []byte
	KeyPEM   []byte
}

// Loader returns all manual certs from persistence (keys already decrypted).
type Loader func(ctx context.Context) ([]Entry, error)

// CertStore serves operator-supplied certificates by SNI, falling back to a
// provided getter (ACME or self-signed) when no manual cert matches. When empty
// it is transparent: GetCertificate == the fallback, so default behaviour is
// unchanged.
type CertStore struct {
	load Loader
	log  *slog.Logger

	mu       sync.RWMutex
	server   *tls.Certificate   // domain_id NULL
	domains  []*tls.Certificate // per-domain, matched by SNI
	fallback func(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

// NewCertStore builds a store with the given loader.
func NewCertStore(load Loader, log *slog.Logger) *CertStore {
	return &CertStore{load: load, log: log}
}

// SetFallback sets the getter used when no manual cert matches.
func (cs *CertStore) SetFallback(f func(*tls.ClientHelloInfo) (*tls.Certificate, error)) {
	cs.mu.Lock()
	cs.fallback = f
	cs.mu.Unlock()
}

// Reload refreshes the in-memory certs from the loader. Invalid entries are
// skipped (logged) so one bad cert can't break TLS.
func (cs *CertStore) Reload(ctx context.Context) error {
	entries, err := cs.load(ctx)
	if err != nil {
		return err
	}
	var server *tls.Certificate
	var domains []*tls.Certificate
	for _, e := range entries {
		c, err := tls.X509KeyPair(e.CertPEM, e.KeyPEM)
		if err != nil {
			if cs.log != nil {
				cs.log.Warn("tls: skip invalid manual cert", "err", err)
			}
			continue
		}
		if len(c.Certificate) > 0 {
			c.Leaf, _ = x509.ParseCertificate(c.Certificate[0])
		}
		cc := c
		if e.DomainID == nil {
			server = &cc
		} else {
			domains = append(domains, &cc)
		}
	}
	cs.mu.Lock()
	cs.server, cs.domains = server, domains
	cs.mu.Unlock()
	return nil
}

// match returns the manual cert for an SNI: a per-domain cert whose leaf covers
// the name, else the server cert (may be nil).
func (cs *CertStore) match(sni string) *tls.Certificate {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	for _, c := range cs.domains {
		if c.Leaf != nil && c.Leaf.VerifyHostname(sni) == nil {
			return c
		}
	}
	return cs.server
}

// GetCertificate resolves a handshake: manual cert if one matches, else fallback.
func (cs *CertStore) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if c := cs.match(hello.ServerName); c != nil {
		return c, nil
	}
	cs.mu.RLock()
	fb := cs.fallback
	cs.mu.RUnlock()
	if fb != nil {
		return fb(hello)
	}
	return nil, fmt.Errorf("no certificate for %q", hello.ServerName)
}

// HasServerCert reports whether a manual server-hostname cert is loaded (so the
// caller can skip ACME issuance).
func (cs *CertStore) HasServerCert() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.server != nil
}

// ServerLeafNotAfter returns the manual server cert's expiry, if present.
func (cs *CertStore) ServerLeafNotAfter() (time.Time, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.server != nil && cs.server.Leaf != nil {
		return cs.server.Leaf.NotAfter, true
	}
	return time.Time{}, false
}

// SMTPTLSConfig serves manual/fallback certs on the SMTP listeners.
func (cs *CertStore) SMTPTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: cs.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites:   hardenedCiphers,
	}
}

// HTTPSTLSConfig serves manual/fallback certs on the HTTPS server (used when
// ACME isn't managing the config, i.e. a manual server cert or self-signed).
func (cs *CertStore) HTTPSTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: cs.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites:   hardenedCiphers,
		NextProtos:     []string{"h2", "http/1.1"},
	}
}
