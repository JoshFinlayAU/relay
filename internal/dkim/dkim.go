// Package dkim handles DKIM key generation, at-rest key storage helpers, and
// the DNS record representation. Signing lives in Phase 3.
package dkim

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

// DefaultKeyBits is the RSA modulus size (CLAUDE.md: RSA-2048 for broad
// verifier compatibility).
const DefaultKeyBits = 2048

// KeyPair is a freshly generated DKIM key.
type KeyPair struct {
	Selector   string // e.g. rly2026a
	PrivatePEM []byte // PKCS#1 PEM, to be encrypted at rest
	PublicB64  string // base64 DER SPKI - the p= value in the DNS record
	Algorithm  string // "rsa"
}

// Selector returns a year-stamped selector, e.g. rly2026a, to make rotation
// sane (a second key in the same year would be rly2026b, etc.).
func Selector(year int, suffix string) string {
	if suffix == "" {
		suffix = "a"
	}
	return fmt.Sprintf("rly%d%s", year, suffix)
}

// Generate creates an RSA-2048 DKIM keypair for the given selector.
func Generate(selector string) (*KeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, DefaultKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	return &KeyPair{
		Selector:   selector,
		PrivatePEM: privPEM,
		PublicB64:  base64.StdEncoding.EncodeToString(pubDER),
		Algorithm:  "rsa",
	}, nil
}

// TXTValue returns the DKIM DNS TXT record value for a public key.
func TXTValue(publicB64 string) string {
	return "v=DKIM1; k=rsa; p=" + publicB64
}

// ParsePublicKey extracts the base64 p= value from a DKIM TXT record's contents.
// Handles the tag=value; format and whitespace/segmentation from resolvers.
func ParsePublicKey(txt string) (string, error) {
	// Resolvers may return the record split into multiple quoted segments;
	// callers should join them first, but tolerate stray whitespace here.
	for _, part := range strings.Split(txt, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "p=") {
			p := strings.TrimSpace(strings.TrimPrefix(part, "p="))
			p = strings.Join(strings.Fields(p), "") // remove any internal whitespace
			if p == "" {
				return "", fmt.Errorf("empty p= tag")
			}
			return p, nil
		}
	}
	return "", fmt.Errorf("no p= tag in DKIM record")
}
