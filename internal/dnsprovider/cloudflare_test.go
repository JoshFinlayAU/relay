package dnsprovider

import "testing"

func TestParseMX(t *testing.T) {
	p, h := parseMX("10 mail.as135559.net.au.")
	if p != 10 || h != "mail.as135559.net.au" {
		t.Errorf("parseMX = %d %q", p, h)
	}
	// Fallback when no priority present.
	if p2, h2 := parseMX("mail.example.com"); p2 != 10 || h2 != "mail.example.com" {
		t.Errorf("parseMX fallback = %d %q", p2, h2)
	}
}
