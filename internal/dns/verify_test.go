package dns

import "testing"

func TestStripNonBase64(t *testing.T) {
	// A base64 value corrupted by a stray "\010  " sequence (paste line-break
	// artifact) reduces back to the clean key.
	clean := "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8="
	corrupted := "MIIBIjANBgkqhkiG9w0B" + `\010  ` + "AQEFAAOCAQ8="
	if got := stripNonBase64(corrupted); got != "MIIBIjANBgkqhkiG9w0B010AQEFAAOCAQ8=" {
		// note: digits 0,1,0 survive (they are base64), only backslash+spaces removed
		t.Errorf("stripNonBase64 = %q", got)
	}
	if stripNonBase64(clean) != clean {
		t.Error("clean base64 should be unchanged")
	}
}

func TestZoneCandidates(t *testing.T) {
	cases := []struct {
		domain, base string
		want         []string
	}{
		{"voxteam.app", "voxteam.app", []string{"voxteam.app"}},
		{"support.athenanetworks.com.au", "athenanetworks.com.au",
			[]string{"support.athenanetworks.com.au", "athenanetworks.com.au"}},
		{"a.b.example.com", "example.com",
			[]string{"a.b.example.com", "b.example.com", "example.com"}},
		{"Support.Example.COM.", "example.com",
			[]string{"support.example.com", "example.com"}},
	}
	for _, c := range cases {
		got := zoneCandidates(c.domain, c.base)
		if len(got) != len(c.want) {
			t.Errorf("zoneCandidates(%q,%q) = %v, want %v", c.domain, c.base, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("zoneCandidates(%q,%q)[%d] = %q, want %q", c.domain, c.base, i, got[i], c.want[i])
			}
		}
	}
}
