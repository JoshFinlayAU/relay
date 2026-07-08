package dkim

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"

	msgdkim "github.com/emersion/go-msgauth/dkim"
)

// DefaultHeaderKeys are the headers Relay signs (CLAUDE.md). Headers not present
// in the message are simply skipped by the signer.
var DefaultHeaderKeys = []string{
	"From", "To", "Subject", "Date", "Message-ID", "MIME-Version",
	"Content-Type", "Reply-To", "List-Unsubscribe",
}

// Signer holds a parsed private key ready to sign for a domain/selector.
type Signer struct {
	domain   string
	selector string
	key      crypto.Signer
}

// NewSigner parses a PKCS#1 PEM private key (as produced by Generate) for
// signing under domain/selector.
func NewSigner(domain, selector string, privPEM []byte) (*Signer, error) {
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return nil, fmt.Errorf("dkim: no PEM block in private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("dkim: parse private key: %w", err)
	}
	return &Signer{domain: domain, selector: selector, key: key}, nil
}

// Sign returns the message with a prepended DKIM-Signature header
// (relaxed/relaxed, SHA-256).
func (s *Signer) Sign(message []byte) ([]byte, error) {
	opts := &msgdkim.SignOptions{
		Domain:                 s.domain,
		Selector:               s.selector,
		Signer:                 s.key,
		Hash:                   crypto.SHA256,
		HeaderCanonicalization: msgdkim.CanonicalizationRelaxed,
		BodyCanonicalization:   msgdkim.CanonicalizationRelaxed,
		HeaderKeys:             DefaultHeaderKeys,
	}
	var out bytes.Buffer
	if err := msgdkim.Sign(&out, bytes.NewReader(message), opts); err != nil {
		return nil, fmt.Errorf("dkim: sign: %w", err)
	}
	return out.Bytes(), nil
}

// Verify checks DKIM signatures on a message (used in tests and inbound).
func Verify(message []byte) ([]*msgdkim.Verification, error) {
	return msgdkim.Verify(io.Reader(bytes.NewReader(message)))
}

// VerifyWithKey checks DKIM signatures using a fixed public key TXT value for
// every lookup - offline verification for tests (no DNS).
func VerifyWithKey(message []byte, txtValue string) ([]*msgdkim.Verification, error) {
	return msgdkim.VerifyWithOptions(bytes.NewReader(message), &msgdkim.VerifyOptions{
		LookupTXT: func(string) ([]string, error) { return []string{txtValue}, nil },
	})
}
