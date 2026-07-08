package delivery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"time"
)

// Result records the outcome of a delivery attempt against one MX host.
type Result struct {
	MXHost      string
	Code        int    // SMTP reply code (0 if connection-level failure)
	Response    string // SMTP reply text or error
	TLSVersion  string // "" if plaintext
	TLSVerified bool
	Delivered   bool
	Permanent   bool // 5xx → do not retry
}

// DeliverInput is a single-recipient delivery request.
type DeliverInput struct {
	MXHosts  []string // in preference order
	EHLOName string   // our server hostname
	MailFrom string   // VERP return path
	Rcpt     string
	Data     []byte // full signed message
	Timeout  time.Duration
	Port     string // destination port; defaults to "25"

	Resolver  *net.Resolver
	LocalIPv4 string // source address to bind for IPv4 targets (PTR/SPF-correct)
	LocalIPv6 string // source address to bind for IPv6 targets
	UseIPv6   bool   // attempt IPv6 targets at all (off until IPv6 DNS is sound)
}

// Deliver tries each MX host (and each of its IPs) in order, binding the
// configured source address per family. It returns on the first success, the
// first permanent (5xx) rejection, or the last temporary error.
func Deliver(ctx context.Context, in DeliverInput) Result {
	resolver := in.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	var last Result
	tried := false
	// Try every candidate (IPv6-first, then IPv4). We do NOT early-return on a
	// permanent 5xx while other-family candidates remain, so an IPv6-specific
	// policy rejection still falls back to IPv4. Delivery short-circuits on the
	// first success; otherwise the last (IPv4-most-authoritative) verdict wins.
	for _, mx := range in.MXHosts {
		for _, ip := range orderedIPs(ctx, resolver, mx, in.UseIPv6) {
			tried = true
			res := deliverToIP(ctx, mx, ip, in)
			if res.Delivered {
				return res
			}
			last = res
		}
	}
	if !tried {
		return Result{Response: "no reachable MX addresses (unresolved)"}
	}
	return last
}

// orderedIPs resolves an MX host to candidate IPs. IPv6 is preferred (RFC 6555
// / CLAUDE.md "IPv6-capable with v4 fallback"); IPv4 always follows as fallback.
// IPv6 is skipped only when explicitly disabled.
func orderedIPs(ctx context.Context, resolver *net.Resolver, host string, useIPv6 bool) []net.IP {
	// Literal IPs (used in tests) bypass resolution.
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil && !useIPv6 {
			return nil
		}
		return []net.IP{ip}
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	var v4, v6 []net.IP
	for _, a := range addrs {
		if a.IP.To4() != nil {
			v4 = append(v4, a.IP)
		} else if useIPv6 {
			v6 = append(v6, a.IP)
		}
	}
	return append(v6, v4...) // IPv6 first, IPv4 fallback
}

func deliverToIP(ctx context.Context, mxHost string, ip net.IP, in DeliverInput) Result {
	res := Result{MXHost: mxHost}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	port := in.Port
	if port == "" {
		port = "25"
	}

	d := net.Dialer{Timeout: timeout, LocalAddr: localAddr(ip, in)}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
	if err != nil {
		res.Response = "connect: " + err.Error()
		return res
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	c, err := smtp.NewClient(conn, mxHost)
	if err != nil {
		res.Response = "smtp init: " + err.Error()
		return res
	}
	defer c.Close()

	if err := c.Hello(in.EHLOName); err != nil {
		return withErr(res, err, "EHLO")
	}
	// Opportunistic STARTTLS: encrypt when offered; record whether the cert
	// verified but never downgrade to plaintext when TLS is available.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: mxHost, InsecureSkipVerify: true}); err == nil { //nolint:gosec // verification recorded separately
			if state, ok := c.TLSConnectionState(); ok {
				res.TLSVersion = tlsVersionName(state.Version)
				res.TLSVerified = verifyChain(state, mxHost)
			}
		}
	}
	if err := c.Mail(in.MailFrom); err != nil {
		return withErr(res, err, "MAIL FROM")
	}
	if err := c.Rcpt(in.Rcpt); err != nil {
		return withErr(res, err, "RCPT TO")
	}
	w, err := c.Data()
	if err != nil {
		return withErr(res, err, "DATA")
	}
	if _, err := w.Write(in.Data); err != nil {
		return withErr(res, err, "write body")
	}
	if err := w.Close(); err != nil {
		return withErr(res, err, "end of DATA")
	}
	_ = c.Quit()

	res.Delivered = true
	res.Code = 250
	res.Response = "accepted"
	return res
}

// localAddr returns the source TCP address to bind for the target IP's family,
// or nil to let the OS choose.
func localAddr(target net.IP, in DeliverInput) *net.TCPAddr {
	var s string
	if target.To4() != nil {
		s = in.LocalIPv4
	} else {
		s = in.LocalIPv6
	}
	if s == "" {
		return nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil
	}
	return &net.TCPAddr{IP: ip}
}

func withErr(res Result, err error, stage string) Result {
	var proto *textproto.Error
	if errors.As(err, &proto) {
		res.Code = proto.Code
		res.Response = fmt.Sprintf("%s: %d %s", stage, proto.Code, proto.Msg)
		res.Permanent = proto.Code >= 500 && proto.Code < 600
		return res
	}
	res.Response = stage + ": " + err.Error()
	res.Permanent = false // transport errors are retryable
	return res
}

func verifyChain(state tls.ConnectionState, host string) bool {
	if len(state.PeerCertificates) == 0 {
		return false
	}
	opts := x509.VerifyOptions{DNSName: host, Intermediates: x509.NewCertPool()}
	for _, cert := range state.PeerCertificates[1:] {
		opts.Intermediates.AddCert(cert)
	}
	_, err := state.PeerCertificates[0].Verify(opts)
	return err == nil
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS1.3"
	case tls.VersionTLS12:
		return "TLS1.2"
	case tls.VersionTLS11:
		return "TLS1.1"
	case tls.VersionTLS10:
		return "TLS1.0"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}
