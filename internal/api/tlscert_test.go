package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"
)

func genCertPEM(t *testing.T, cn string, names ...string) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		DNSNames:  append([]string{cn}, names...),
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	kp := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}))
	return cert, kp
}

func TestDomainTLSCertLifecycle(t *testing.T) {
	ts := newTestServer(t)
	did := createDomainForTest(t, ts.URL, testToken, "tlsdom.example")

	// Not configured initially.
	_, out := do(t, "GET", ts.URL+"/v1/domains/"+did+"/tls-cert", testToken, nil)
	if out["configured"] != false {
		t.Errorf("expected not configured, got %v", out)
	}

	// Invalid cert rejected.
	st, _ := do(t, "PUT", ts.URL+"/v1/domains/"+did+"/tls-cert", testToken,
		map[string]any{"cert_pem": "not a cert", "key_pem": "nope"})
	if st != http.StatusBadRequest {
		t.Errorf("invalid cert = %d, want 400", st)
	}

	// Upload a valid cert.
	cert, key := genCertPEM(t, "mail.tlsdom.example", "tlsdom.example")
	st, put := do(t, "PUT", ts.URL+"/v1/domains/"+did+"/tls-cert", testToken,
		map[string]any{"cert_pem": cert, "key_pem": key})
	if st != http.StatusOK {
		t.Fatalf("put cert = %d (%v)", st, put)
	}
	subs, _ := put["subjects"].([]any)
	if len(subs) < 1 {
		t.Errorf("subjects not returned: %v", put)
	}

	// Now configured; key/secret never returned.
	_, got := do(t, "GET", ts.URL+"/v1/domains/"+did+"/tls-cert", testToken, nil)
	if got["configured"] != true {
		t.Errorf("expected configured after upload, got %v", got)
	}
	if _, leaked := got["key_pem"]; leaked {
		t.Error("must not return the private key")
	}

	// Delete.
	if st, _ := do(t, "DELETE", ts.URL+"/v1/domains/"+did+"/tls-cert", testToken, nil); st != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", st)
	}
	_, after := do(t, "GET", ts.URL+"/v1/domains/"+did+"/tls-cert", testToken, nil)
	if after["configured"] != false {
		t.Errorf("expected not configured after delete, got %v", after)
	}
}
