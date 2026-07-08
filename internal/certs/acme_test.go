package certs

import (
	"context"
	"crypto/tls"
	"testing"
)

func TestHardenedTLSConfigs(t *testing.T) {
	m, err := NewManager(context.Background(), ACMEConfig{
		Hostname: "mail.test", Email: "ops@test", StorageDir: t.TempDir(), Staging: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	https := m.HTTPSTLSConfig()
	if https.MinVersion != tls.VersionTLS12 {
		t.Errorf("https MinVersion = %x, want TLS1.2", https.MinVersion)
	}
	if https.GetCertificate == nil {
		t.Error("https config missing GetCertificate")
	}
	// ALPN advertises HTTP for the browser/API server.
	foundH2 := false
	for _, p := range https.NextProtos {
		if p == "h2" {
			foundH2 = true
		}
	}
	if !foundH2 {
		t.Error("https config should advertise h2")
	}

	smtp := m.SMTPTLSConfig()
	if smtp.MinVersion != tls.VersionTLS12 || smtp.GetCertificate == nil {
		t.Error("smtp TLS config not hardened / missing cert source")
	}
	if len(smtp.CipherSuites) == 0 {
		t.Error("smtp config should pin cipher suites")
	}
}

func TestSelfSignedStillWorks(t *testing.T) {
	c, err := SelfSigned("mail.test")
	if err != nil || len(c.Certificate) == 0 {
		t.Fatalf("self-signed: %v", err)
	}
}
