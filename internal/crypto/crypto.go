// Package crypto provides AES-256-GCM encryption for secrets stored at rest
// (DKIM private keys, webhook secrets). The 32-byte key comes from config
// (RELAY_SECRET_KEY, base64). Ciphertext layout: nonce || sealed.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// ErrKeyLen is returned when the configured key is not 32 bytes.
var ErrKeyLen = errors.New("secret key must be 32 bytes (base64-encoded)")

// Sealer encrypts and decrypts small secrets with a fixed AES-256-GCM key, and
// signs short-lived URL tokens with a domain-separated HMAC key derived from the
// same secret.
type Sealer struct {
	aead    cipher.AEAD
	hmacKey []byte
}

// NewSealer builds a Sealer from a base64-encoded 32-byte key.
func NewSealer(keyB64 string) (*Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("decode secret key: %w", err)
	}
	if len(key) != 32 {
		return nil, ErrKeyLen
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Domain-separated HMAC key so URL signing never uses the AES key directly.
	hk := hmac.New(sha256.New, key)
	hk.Write([]byte("relay-url-signing-v1"))
	return &Sealer{aead: aead, hmacKey: hk.Sum(nil)}, nil
}

// SignHex returns the hex HMAC-SHA256 of msg (for signed URLs / tokens).
func (s *Sealer) SignHex(msg string) string {
	m := hmac.New(sha256.New, s.hmacKey)
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// VerifyHex reports whether sigHex is a valid signature for msg (constant-time).
func (s *Sealer) VerifyHex(msg, sigHex string) bool {
	want, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, s.hmacKey)
	m.Write([]byte(msg))
	return hmac.Equal(want, m.Sum(nil))
}

// Seal encrypts plaintext, returning nonce||ciphertext.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts nonce||ciphertext produced by Seal.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(sealed) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := sealed[:ns], sealed[ns:]
	return s.aead.Open(nil, nonce, ct, nil)
}
