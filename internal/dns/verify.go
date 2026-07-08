package dns

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/publicsuffix"

	dkimpkg "relay/internal/dkim"
)

// Result is the outcome of a single record check.
type Result string

const (
	ResultPass    Result = "pass"
	ResultFail    Result = "fail"
	ResultWarn    Result = "warn"
	ResultUnknown Result = "unknown"
)

// RecordResult is a per-record verification outcome.
type RecordResult struct {
	Purpose  Purpose `json:"purpose"`
	Result   Result  `json:"result"`
	Observed string  `json:"observed"`
	Detail   string  `json:"detail"`
}

// Verifier runs live DNS checks against a domain's authoritative nameservers.
type Verifier struct {
	params    Params
	bootstrap []string // recursive resolvers (host:port) for NS discovery
	timeout   time.Duration
}

// NewVerifier builds a Verifier. bootstrap resolvers are used only to discover
// the domain's authoritative nameservers; record checks then query those NS
// directly (per CLAUDE.md: not the local resolver cache).
func NewVerifier(p Params, bootstrap []string) *Verifier {
	if len(bootstrap) == 0 {
		bootstrap = []string{"1.1.1.1:53", "8.8.8.8:53"}
	}
	return &Verifier{params: p, bootstrap: bootstrap, timeout: 5 * time.Second}
}

func (v *Verifier) exchange(ctx context.Context, server, qname string, qtype uint16, rd bool) (*dns.Msg, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = rd
	m.SetEdns0(4096, false) // advertise a large UDP buffer to avoid truncation
	c := &dns.Client{Timeout: v.timeout}
	in, _, err := c.ExchangeContext(ctx, m, server)
	// Fall back to TCP if the UDP answer was truncated (e.g. an apex with many
	// TXT records: verification tokens + SPF + DKIM easily exceed 512 bytes).
	if err == nil && in != nil && in.Truncated {
		tcp := &dns.Client{Timeout: v.timeout, Net: "tcp"}
		if tin, _, terr := tcp.ExchangeContext(ctx, m, server); terr == nil {
			return tin, nil
		}
	}
	return in, err
}

// authoritativeServers finds the authoritative NS IPs for a domain's
// registrable (eTLD+1) zone.
func (v *Verifier) authoritativeServers(ctx context.Context, domain string) ([]string, error) {
	base, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(domain, "."))
	if err != nil {
		base = domain
	}
	var lastErr error
	for _, boot := range v.bootstrap {
		msg, err := v.exchange(ctx, boot, base, dns.TypeNS, true)
		if err != nil {
			lastErr = err
			continue
		}
		var servers []string
		for _, rr := range msg.Answer {
			ns, ok := rr.(*dns.NS)
			if !ok {
				continue
			}
			// Resolve the NS hostname to an IP (via bootstrap).
			for _, qt := range []uint16{dns.TypeA, dns.TypeAAAA} {
				a, err := v.exchange(ctx, boot, ns.Ns, qt, true)
				if err != nil {
					continue
				}
				for _, arr := range a.Answer {
					switch x := arr.(type) {
					case *dns.A:
						servers = append(servers, x.A.String()+":53")
					case *dns.AAAA:
						servers = append(servers, "["+x.AAAA.String()+"]:53")
					}
				}
			}
		}
		if len(servers) > 0 {
			return servers, nil
		}
		lastErr = fmt.Errorf("no authoritative servers found for %s", base)
	}
	return nil, lastErr
}

// authQuery queries all authoritative servers for a record, returning the first
// successful answer.
func (v *Verifier) authQuery(ctx context.Context, servers []string, qname string, qtype uint16) (*dns.Msg, error) {
	var lastErr error
	for _, s := range servers {
		msg, err := v.exchange(ctx, s, qname, qtype, false)
		if err != nil {
			lastErr = err
			continue
		}
		return msg, nil
	}
	return nil, lastErr
}

// txtStrings joins each TXT RR's segments into one string per record.
func txtStrings(msg *dns.Msg) []string {
	var out []string
	for _, rr := range msg.Answer {
		if t, ok := rr.(*dns.TXT); ok {
			out = append(out, strings.Join(t.Txt, ""))
		}
	}
	return out
}

// VerifyInput describes what to verify for a domain.
type VerifyInput struct {
	Domain     string
	Token      string
	Selector   string
	DKIMPubB64 string
	Receiving  bool
}

// Verify runs all record checks and returns one result per planned record.
func (v *Verifier) Verify(ctx context.Context, in VerifyInput) ([]RecordResult, error) {
	servers, err := v.authoritativeServers(ctx, in.Domain)
	if err != nil {
		return nil, fmt.Errorf("find authoritative nameservers: %w", err)
	}

	results := []RecordResult{
		v.checkOwnership(ctx, servers, in),
		v.checkDKIM(ctx, servers, in),
		v.checkSPF(ctx, servers, in),
		v.checkDMARC(ctx, servers, in),
		v.checkMX(ctx, servers, BounceSubdomain(in.Domain), PurposeBounceMX),
		v.checkBounceSPF(ctx, servers, in),
	}
	if in.Receiving {
		results = append(results, v.checkMX(ctx, servers, in.Domain, PurposeInboundMX))
	}
	return results, nil
}

func (v *Verifier) checkOwnership(ctx context.Context, servers []string, in VerifyInput) RecordResult {
	r := RecordResult{Purpose: PurposeOwnership}
	msg, err := v.authQuery(ctx, servers, OwnershipName(in.Domain), dns.TypeTXT)
	if err != nil {
		return fail(r, "", "query failed: "+err.Error())
	}
	want := OwnershipValue(in.Token)
	for _, t := range txtStrings(msg) {
		if strings.TrimSpace(t) == want {
			r.Result = ResultPass
			r.Observed = t
			return r
		}
	}
	return fail(r, strings.Join(txtStrings(msg), " | "), "ownership token not found")
}

func (v *Verifier) checkDKIM(ctx context.Context, servers []string, in VerifyInput) RecordResult {
	r := RecordResult{Purpose: PurposeDKIM}
	msg, err := v.authQuery(ctx, servers, DKIMName(in.Selector, in.Domain), dns.TypeTXT)
	if err != nil {
		return fail(r, "", "query failed: "+err.Error())
	}
	for _, t := range txtStrings(msg) {
		pub, perr := dkimpkg.ParsePublicKey(t)
		if perr != nil {
			continue
		}
		r.Observed = "p=" + truncate(pub, 24)
		if pub == in.DKIMPubB64 {
			r.Result = ResultPass
			return r
		}
		// Distinguish "right key, corrupted by stray characters" from a genuinely
		// different key. All RSA-2048 SPKI keys share a ~44-char ASN.1 header, so
		// a long shared prefix (into the modulus) means it's our key with junk
		// spliced in (e.g. a line break inserted when pasting). Receivers reject
		// this too, so it must be fixed.
		if commonPrefixLen(pub, in.DKIMPubB64) >= 80 || stripNonBase64(pub) == in.DKIMPubB64 {
			return fail(r, r.Observed,
				"DKIM record contains stray characters (a line break was likely inserted when pasting) - re-enter the p= value as a single unbroken line")
		}
		return fail(r, r.Observed, "DKIM public key does not match the generated key")
	}
	return fail(r, strings.Join(txtStrings(msg), " | "), "no DKIM record found")
}

// spfLookup returns a TXT resolver that queries the domain's authoritative NS
// for names within its zone and a recursive resolver for out-of-zone includes.
func (v *Verifier) spfLookup(ctx context.Context, servers []string, zone string) lookupTXTFunc {
	z := strings.TrimSuffix(strings.ToLower(zone), ".")
	return func(name string) ([]string, error) {
		n := strings.TrimSuffix(strings.ToLower(name), ".")
		if n == z || strings.HasSuffix(n, "."+z) {
			msg, err := v.authQuery(ctx, servers, name, dns.TypeTXT)
			if err != nil {
				return nil, err
			}
			return txtStrings(msg), nil
		}
		msg, err := v.exchange(ctx, v.bootstrap[0], name, dns.TypeTXT, true)
		if err != nil {
			return nil, err
		}
		return txtStrings(msg), nil
	}
}

func (v *Verifier) checkSPF(ctx context.Context, servers []string, in VerifyInput) RecordResult {
	r := RecordResult{Purpose: PurposeSPF}
	lookup := v.spfLookup(ctx, servers, in.Domain)

	res := evaluateSPF(in.Domain, v.params.SPFInclude, lookup)
	// Observe the apex SPF record for display.
	if apex, err := lookup(in.Domain); err == nil {
		r.Observed = spfRecord(apex)
	}
	switch {
	case !res.found && res.detail != "":
		return fail(r, r.Observed, res.detail)
	case !res.found:
		return fail(r, r.Observed, fmt.Sprintf("include:%s not found in SPF tree", v.params.SPFInclude))
	case res.overLimit:
		r.Result = ResultWarn
		r.Detail = fmt.Sprintf("include present but SPF exceeds the 10 DNS-lookup limit (%d)", res.lookups)
		return r
	default:
		r.Result = ResultPass
		if res.lookups >= 8 {
			r.Detail = fmt.Sprintf("include present; %d/10 SPF lookups used", res.lookups)
		}
		return r
	}
}

// checkBounceSPF verifies the recommended SPF on the bounce subdomain. Missing
// is a warning (not a failure) - DMARC still passes via DKIM alignment.
func (v *Verifier) checkBounceSPF(ctx context.Context, servers []string, in VerifyInput) RecordResult {
	r := RecordResult{Purpose: PurposeBounceSPF}
	name := BounceSubdomain(in.Domain)
	lookup := v.spfLookup(ctx, servers, in.Domain)
	if apex, err := lookup(name); err == nil {
		r.Observed = spfRecord(apex)
	}
	res := evaluateSPF(name, v.params.SPFInclude, lookup)
	switch {
	case res.found && !res.overLimit:
		r.Result = ResultPass
		return r
	case res.found && res.overLimit:
		return warn(r, r.Observed, fmt.Sprintf("include present but SPF exceeds the 10-lookup limit (%d)", res.lookups))
	default:
		return warn(r, r.Observed, "recommended: publish SPF on the bounce subdomain so the envelope also passes SPF")
	}
}

func (v *Verifier) checkDMARC(ctx context.Context, servers []string, in VerifyInput) RecordResult {
	r := RecordResult{Purpose: PurposeDMARC}
	msg, err := v.authQuery(ctx, servers, "_dmarc."+in.Domain, dns.TypeTXT)
	if err != nil {
		return warn(r, "", "query failed: "+err.Error())
	}
	for _, t := range txtStrings(msg) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=dmarc1") {
			r.Result = ResultPass
			r.Observed = t
			return r
		}
	}
	// DMARC is recommended, not required → warn rather than fail.
	return warn(r, "", "no DMARC record found (recommended)")
}

func (v *Verifier) checkMX(ctx context.Context, servers []string, name string, purpose Purpose) RecordResult {
	r := RecordResult{Purpose: purpose}
	msg, err := v.authQuery(ctx, servers, name, dns.TypeMX)
	if err != nil {
		return fail(r, "", "query failed: "+err.Error())
	}
	want := strings.TrimSuffix(strings.ToLower(v.params.Hostname), ".")
	var observed []string
	for _, rr := range msg.Answer {
		if mx, ok := rr.(*dns.MX); ok {
			observed = append(observed, fmt.Sprintf("%d %s", mx.Preference, mx.Mx))
			if strings.TrimSuffix(strings.ToLower(mx.Mx), ".") == want {
				r.Result = ResultPass
				r.Observed = fmt.Sprintf("%d %s", mx.Preference, mx.Mx)
				return r
			}
		}
	}
	return fail(r, strings.Join(observed, " | "), "MX does not point to "+v.params.Hostname)
}

func fail(r RecordResult, observed, detail string) RecordResult {
	r.Result = ResultFail
	r.Observed = observed
	r.Detail = detail
	return r
}

func warn(r RecordResult, observed, detail string) RecordResult {
	r.Result = ResultWarn
	r.Observed = observed
	r.Detail = detail
	return r
}

// commonPrefixLen returns the number of leading bytes a and b share.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// stripNonBase64 removes any character outside the base64 alphabet, used to
// tell "wrong key" apart from "right key, corrupted by stray characters".
func stripNonBase64(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
