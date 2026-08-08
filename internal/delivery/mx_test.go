package delivery

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startTestDNS spins up an in-process UDP DNS server implementing just enough to
// exercise ResolveMX's classification, and returns a *net.Resolver wired to it.
// The handler decides responses per query name:
//   - "good."    → MX 10 mx.good.  (deliverable)
//   - "nullmx."  → MX 0 .          (RFC 7505 null MX → permanent)
//   - "aonly."   → no MX, A 192.0.2.1 (implicit MX fallback)
//   - "missing." → NXDOMAIN for both MX and A (permanent no-destination)
//   - anything else → SERVFAIL (transient)
func startTestDNS(t *testing.T) *net.Resolver {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc}
	srv.Handler = dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		name := strings.ToLower(q.Name)
		switch {
		case name == "good." && q.Qtype == dns.TypeMX:
			m.Answer = append(m.Answer, &dns.MX{
				Hdr:        dns.RR_Header{Name: name, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 60},
				Preference: 10, Mx: "mx.good.",
			})
		case name == "nullmx." && q.Qtype == dns.TypeMX:
			m.Answer = append(m.Answer, &dns.MX{
				Hdr:        dns.RR_Header{Name: name, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 60},
				Preference: 0, Mx: ".",
			})
		case name == "aonly." && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA):
			if q.Qtype == dns.TypeA {
				m.Answer = append(m.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP("192.0.2.1"),
				})
			}
		case name == "missing." || name == "aonly." || name == "nullmx." || name == "good.":
			// Authoritative "no such record" for the query types not answered above
			// (e.g. missing.'s MX and A, good.'s A). NXDOMAIN/NODATA → Rcode 0 w/ no answers
			// is treated as not-found by the Go resolver.
			m.Rcode = dns.RcodeNameError // NXDOMAIN
		default:
			m.Rcode = dns.RcodeServerFailure // SERVFAIL → transient
		}
		_ = w.WriteMsg(m)
	})
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })

	addr := pc.LocalAddr().String()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

func TestResolveMXClassification(t *testing.T) {
	res := startTestDNS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("deliverable MX", func(t *testing.T) {
		hosts, err := ResolveMX(ctx, res, "good")
		if err != nil || len(hosts) != 1 || hosts[0] != "mx.good" {
			t.Fatalf("hosts=%v err=%v", hosts, err)
		}
	})

	t.Run("implicit MX via A record", func(t *testing.T) {
		hosts, err := ResolveMX(ctx, res, "aonly")
		if err != nil || len(hosts) != 1 || hosts[0] != "aonly" {
			t.Fatalf("hosts=%v err=%v", hosts, err)
		}
	})

	t.Run("null MX is permanent", func(t *testing.T) {
		_, err := ResolveMX(ctx, res, "nullmx")
		if !errors.Is(err, ErrNoMailDestination) {
			t.Fatalf("want ErrNoMailDestination, got %v", err)
		}
	})

	t.Run("no MX and no A is permanent", func(t *testing.T) {
		_, err := ResolveMX(ctx, res, "missing")
		if !errors.Is(err, ErrNoMailDestination) {
			t.Fatalf("want ErrNoMailDestination, got %v", err)
		}
	})

	t.Run("SERVFAIL is transient (not permanent)", func(t *testing.T) {
		_, err := ResolveMX(ctx, res, "broken")
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrNoMailDestination) {
			t.Fatalf("SERVFAIL must not be classified permanent: %v", err)
		}
	})
}
