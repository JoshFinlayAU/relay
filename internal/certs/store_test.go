package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
)

// genCert makes a self-signed cert/key PEM pair for the given DNS names.
func genCert(t *testing.T, names ...string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})
	return
}

func serve(cs *CertStore, sni string) *tls.Certificate {
	c, _ := cs.GetCertificate(&tls.ClientHelloInfo{ServerName: sni})
	return c
}

func TestCertStoreSNI(t *testing.T) {
	did := uuid.New()
	srvCert, srvKey := genCert(t, "mail.example.com")
	domCert, domKey := genCert(t, "mail.customer.com", "customer.com")
	fbCert, fbKey := genCert(t, "fallback.example")
	fbPair, _ := tls.X509KeyPair(fbCert, fbKey)

	loader := func(context.Context) ([]Entry, error) {
		return []Entry{
			{CertPEM: srvCert, KeyPEM: srvKey},                 // server (nil domain)
			{DomainID: &did, CertPEM: domCert, KeyPEM: domKey}, // per-domain
		}, nil
	}
	cs := NewCertStore(loader, nil)
	cs.SetFallback(func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &fbPair, nil })
	if err := cs.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	// SNI matching the per-domain cert → that cert.
	if got := serve(cs, "mail.customer.com"); got == nil || got.Leaf.Subject.CommonName != "mail.customer.com" {
		t.Errorf("customer SNI did not resolve to the per-domain cert")
	}
	// Any other SNI → the manual server cert (not the fallback).
	if got := serve(cs, "mail.example.com"); got == nil || got.Leaf.Subject.CommonName != "mail.example.com" {
		t.Errorf("server SNI did not resolve to the server cert")
	}
	if got := serve(cs, "random.host"); got == nil || got.Leaf.Subject.CommonName != "mail.example.com" {
		t.Errorf("unmatched SNI should fall to the server cert, got %v", got)
	}
	if !cs.HasServerCert() {
		t.Error("HasServerCert should be true")
	}
}

func TestCertStoreEmptyUsesFallback(t *testing.T) {
	fbCert, fbKey := genCert(t, "fallback.example")
	fbPair, _ := tls.X509KeyPair(fbCert, fbKey)
	cs := NewCertStore(func(context.Context) ([]Entry, error) { return nil, nil }, nil)
	cs.SetFallback(func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &fbPair, nil })
	_ = cs.Reload(context.Background())

	// With nothing loaded, every SNI goes to the fallback — behaviour unchanged.
	if got := serve(cs, "anything"); got == nil || got.Leaf == nil || got.Leaf.Subject.CommonName != "fallback.example" {
		t.Errorf("empty store should serve the fallback")
	}
	if cs.HasServerCert() {
		t.Error("HasServerCert should be false when empty")
	}
}
