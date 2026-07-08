// Package crypto provides AES-256-GCM encryption for secrets stored at rest
// (DKIM private keys, webhook secrets). The 32-byte key comes from config
// (RELAY_SECRET_KEY, base64). Ciphertext layout: nonce || sealed.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrKeyLen is returned when the configured key is not 32 bytes.
var ErrKeyLen = errors.New("secret key must be 32 bytes (base64-encoded)")

// Sealer encrypts and decrypts small secrets with a fixed AES-256-GCM key.
type Sealer struct {
	aead cipher.AEAD
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
	return &Sealer{aead: aead}, nil
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
