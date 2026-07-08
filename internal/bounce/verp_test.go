package bounce

import (
	"testing"

	"github.com/google/uuid"
)

func TestVERPRoundTrip(t *testing.T) {
	id := uuid.New()
	addr := VERPAddress(id, "bounce.example.com")
	want := "bounce-" + id.String() + "@bounce.example.com"
	if addr != want {
		t.Fatalf("VERPAddress = %q, want %q", addr, want)
	}
	got, ok := DecodeVERP(addr)
	if !ok || got != id {
		t.Fatalf("DecodeVERP(%q) = %v, %v", addr, got, ok)
	}
}

func TestDecodeVERPNonMatching(t *testing.T) {
	for _, addr := range []string{
		"support@example.com",
		"bounce-not-a-uuid@bounce.example.com",
		"noatsign",
		"bounce-@bounce.example.com",
	} {
		if _, ok := DecodeVERP(addr); ok {
			t.Errorf("DecodeVERP(%q) should be false", addr)
		}
	}
}
