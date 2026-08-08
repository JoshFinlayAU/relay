// Package delivery is the outbound engine: it claims queued jobs, resolves MX
// hosts, delivers over opportunistic-TLS SMTP, and applies the retry policy.
package delivery

import (
	"context"
	"errors"
	"net"
	"sort"
)

// ErrNoMailDestination means the domain authoritatively cannot receive mail:
// it has no usable MX record and no A/AAAA fallback (both lookups returned
// NXDOMAIN / no-such-host), or it publishes an RFC 7505 null MX ("."). This is
// a permanent condition — retrying for 72h cannot fix it — so the caller should
// bounce immediately rather than defer.
var ErrNoMailDestination = errors.New("no mail destination (no MX or address record)")

// ResolveMX returns candidate mail hosts for a domain in preference order,
// falling back to the domain's A/AAAA record if no MX exists (RFC 5321 §5.1).
//
// It returns ErrNoMailDestination when the absence is authoritative (permanent),
// and a transient error (resolver timeout, SERVFAIL) otherwise so the job defers.
func ResolveMX(ctx context.Context, resolver *net.Resolver, domain string) ([]string, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	mxs, mxErr := resolver.LookupMX(ctx, domain)
	if mxErr == nil && len(mxs) > 0 {
		sort.SliceStable(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
		hosts := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			h := trimDot(mx.Host)
			if h != "" {
				hosts = append(hosts, h)
			}
		}
		if len(hosts) > 0 {
			return hosts, nil
		}
		// MX records exist but the only target is the root ("."): RFC 7505 null
		// MX — the domain explicitly does not accept mail. Permanent.
		return nil, ErrNoMailDestination
	}

	// Implicit MX: fall back to the domain's address records.
	if _, aerr := resolver.LookupHost(ctx, domain); aerr == nil {
		return []string{domain}, nil
	} else if isNotFound(mxErr) && isNotFound(aerr) {
		// Neither MX nor A/AAAA exist, both answers authoritative. Permanent.
		return nil, ErrNoMailDestination
	} else if mxErr != nil && !isNotFound(mxErr) {
		// A transient MX-lookup failure is the more informative error to surface.
		return nil, mxErr
	} else {
		return nil, aerr
	}
}

// isNotFound reports whether err is an authoritative "no such record" DNS answer
// (NXDOMAIN or no records of the requested type), as opposed to a transient
// failure like a timeout or SERVFAIL.
func isNotFound(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsNotFound
	}
	return false
}

func trimDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
