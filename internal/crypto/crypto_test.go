package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := NewSealer(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("super secret DKIM private key material")
	sealed, err := s.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("sealed output contains plaintext")
	}
	got, err := s.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestSealNonceUnique(t *testing.T) {
	s, _ := NewSealer(newKey(t))
	a, _ := s.Seal([]byte("x"))
	b, _ := s.Seal([]byte("x"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals produced identical ciphertext (nonce reuse)")
	}
}

func TestBadKey(t *testing.T) {
	if _, err := NewSealer("nope"); err == nil {
		t.Fatal("expected error for non-base64 key")
	}
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := NewSealer(short); err != ErrKeyLen {
		t.Fatalf("expected ErrKeyLen, got %v", err)
	}
}

func TestOpenTampered(t *testing.T) {
	s, _ := NewSealer(newKey(t))
	sealed, _ := s.Seal([]byte("data"))
	sealed[len(sealed)-1] ^= 0xff
	if _, err := s.Open(sealed); err == nil {
		t.Fatal("expected auth failure on tampered ciphertext")
	}
}
