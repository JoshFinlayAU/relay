package dns

import "testing"

func TestEvaluateSPF(t *testing.T) {
	zone := map[string][]string{
		"example.com":           {"v=spf1 include:spf.relay.test include:_spf.google.com ~all"},
		"spf.relay.test":        {"v=spf1 ip4:160.30.37.130 ip6:2001:df4:2040:5::2 -all"},
		"_spf.google.com":       {"v=spf1 include:_netblocks.google.com ~all"},
		"_netblocks.google.com": {"v=spf1 ip4:1.2.3.0/24 ~all"},
	}
	lookup := func(name string) ([]string, error) { return zone[name], nil }

	res := evaluateSPF("example.com", "spf.relay.test", lookup)
	if !res.found {
		t.Fatal("expected include target to be found")
	}
	// include:spf.relay.test, include:_spf.google.com, include:_netblocks.google.com = 3 lookups
	if res.lookups != 3 {
		t.Errorf("lookups = %d, want 3", res.lookups)
	}
	if res.overLimit {
		t.Error("should not be over limit")
	}
}

func TestEvaluateSPFNotFound(t *testing.T) {
	zone := map[string][]string{
		"example.com":        {"v=spf1 include:other.provider.net ~all"},
		"other.provider.net": {"v=spf1 ip4:9.9.9.9 ~all"},
	}
	res := evaluateSPF("example.com", "spf.relay.test", func(n string) ([]string, error) { return zone[n], nil })
	if res.found {
		t.Fatal("include target should not be found")
	}
}

func TestEvaluateSPFOverLimit(t *testing.T) {
	// A record that includes 11 lookup mechanisms.
	zone := map[string][]string{
		"example.com":    {"v=spf1 include:spf.relay.test a mx a:h1 a:h2 a:h3 a:h4 a:h5 a:h6 a:h7 exists:x ~all"},
		"spf.relay.test": {"v=spf1 ip4:1.1.1.1 -all"},
	}
	res := evaluateSPF("example.com", "spf.relay.test", func(n string) ([]string, error) { return zone[n], nil })
	if !res.found {
		t.Fatal("include should be found")
	}
	if !res.overLimit {
		t.Errorf("expected over-limit, lookups=%d", res.lookups)
	}
}

func TestEvaluateSPFRedirect(t *testing.T) {
	zone := map[string][]string{
		"example.com":    {"v=spf1 redirect=spf.relay.test"},
		"spf.relay.test": {"v=spf1 ip4:1.1.1.1 -all"},
	}
	res := evaluateSPF("example.com", "spf.relay.test", func(n string) ([]string, error) { return zone[n], nil })
	if !res.found {
		t.Fatal("redirect target should count as found")
	}
}

func TestSpfRecordSelection(t *testing.T) {
	got := spfRecord([]string{"some other txt", "v=spf1 include:x ~all"})
	if got != "v=spf1 include:x ~all" {
		t.Errorf("got %q", got)
	}
	if spfRecord([]string{"not spf"}) != "" {
		t.Error("expected empty when no spf record")
	}
}
