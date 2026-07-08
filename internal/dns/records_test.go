package dns

import (
	"strings"
	"testing"
)

func testParams() Params {
	return Params{
		Hostname:   "mail.as135559.net.au",
		SPFInclude: "spf.mail.as135559.net.au",
		DMARCRua:   "mailto:dmarc@as135559.net.au",
	}
}

func TestPlanRecordsNonReceiving(t *testing.T) {
	recs := PlanRecords("voxteam.app", "tok123", "rly2026a", "PUBKEY", false, testParams())
	if len(recs) != 6 {
		t.Fatalf("want 6 records, got %d", len(recs))
	}
	byPurpose := map[Purpose]RecordSpec{}
	for _, r := range recs {
		byPurpose[r.Purpose] = r
	}
	if got := byPurpose[PurposeOwnership].Name; got != "_relay-verify.voxteam.app" {
		t.Errorf("ownership name %q", got)
	}
	if got := byPurpose[PurposeOwnership].Value; got != "relay-verify=tok123" {
		t.Errorf("ownership value %q", got)
	}
	if got := byPurpose[PurposeDKIM].Name; got != "rly2026a._domainkey.voxteam.app" {
		t.Errorf("dkim name %q", got)
	}
	if !strings.Contains(byPurpose[PurposeSPF].Value, "include:spf.mail.as135559.net.au") {
		t.Errorf("spf value %q", byPurpose[PurposeSPF].Value)
	}
	if got := byPurpose[PurposeBounceMX].Name; got != "bounce.voxteam.app" {
		t.Errorf("bounce name %q", got)
	}
	if got := byPurpose[PurposeBounceMX].Value; got != "10 mail.as135559.net.au." {
		t.Errorf("bounce mx %q", got)
	}
	if _, ok := byPurpose[PurposeInboundMX]; ok {
		t.Error("inbound MX should be absent when receiving=false")
	}
}

func TestPlanRecordsReceiving(t *testing.T) {
	recs := PlanRecords("voxteam.app", "t", "rly2026a", "PUB", true, testParams())
	found := false
	for _, r := range recs {
		if r.Purpose == PurposeInboundMX {
			found = true
			if r.Name != "voxteam.app" {
				t.Errorf("inbound mx name %q", r.Name)
			}
		}
	}
	if !found {
		t.Error("expected inbound MX when receiving=true")
	}
}

func TestZoneLine(t *testing.T) {
	txt := RecordSpec{Type: "TXT", Name: "_dmarc.voxteam.app", Value: "v=DMARC1; p=none"}
	if got := txt.ZoneLine(); got != `_dmarc.voxteam.app. IN TXT "v=DMARC1; p=none"` {
		t.Errorf("txt zone line %q", got)
	}
	mx := RecordSpec{Type: "MX", Name: "bounce.voxteam.app", Value: "10 mail.as135559.net.au."}
	if got := mx.ZoneLine(); got != "bounce.voxteam.app. IN MX 10 mail.as135559.net.au." {
		t.Errorf("mx zone line %q", got)
	}
}

func TestMergeSPFExported(t *testing.T) {
	got := MergeSPF("v=spf1 a mx include:spf.postal.example ~all", "spf.mail.as135559.net.au")
	want := "v=spf1 a mx include:spf.postal.example include:spf.mail.as135559.net.au ~all"
	if got != want {
		t.Errorf("MergeSPF = %q", got)
	}
	// No trailing all → include appended at end.
	if g := MergeSPF("v=spf1 ip4:1.2.3.4", "spf.mail.as135559.net.au"); g != "v=spf1 ip4:1.2.3.4 include:spf.mail.as135559.net.au" {
		t.Errorf("MergeSPF no-all = %q", g)
	}
}
