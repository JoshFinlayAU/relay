package dkim

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
)

func TestSelector(t *testing.T) {
	if got := Selector(2026, ""); got != "rly2026a" {
		t.Errorf("Selector = %q", got)
	}
	if got := Selector(2026, "b"); got != "rly2026b" {
		t.Errorf("Selector = %q", got)
	}
}

func TestGenerateAndParse(t *testing.T) {
	kp, err := Generate("rly2026a")
	if err != nil {
		t.Fatal(err)
	}
	// Private key decodes as a valid RSA-2048 PKCS#1 key.
	block, _ := pem.Decode(kp.PrivatePEM)
	if block == nil {
		t.Fatal("private PEM did not decode")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if priv.N.BitLen() != DefaultKeyBits {
		t.Errorf("key size = %d, want %d", priv.N.BitLen(), DefaultKeyBits)
	}

	// Public value is valid DER SPKI and round-trips through ParsePublicKey.
	der, err := base64.StdEncoding.DecodeString(kp.PublicB64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x509.ParsePKIXPublicKey(der); err != nil {
		t.Fatal(err)
	}

	txt := TXTValue(kp.PublicB64)
	got, err := ParsePublicKey(txt)
	if err != nil {
		t.Fatal(err)
	}
	if got != kp.PublicB64 {
		t.Errorf("parsed p= mismatch")
	}
}

func TestParsePublicKeyWhitespace(t *testing.T) {
	// Simulate a resolver that inserted whitespace into the long p= value.
	got, err := ParsePublicKey("v=DKIM1; k=rsa; p=AAAA BBBB\tCCCC")
	if err != nil {
		t.Fatal(err)
	}
	if got != "AAAABBBBCCCC" {
		t.Errorf("got %q", got)
	}
}

func TestParsePublicKeyMissing(t *testing.T) {
	if _, err := ParsePublicKey("v=DKIM1; k=rsa"); err == nil {
		t.Fatal("expected error when p= absent")
	}
}
