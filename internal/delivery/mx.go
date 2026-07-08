// Package delivery is the outbound engine: it claims queued jobs, resolves MX
// hosts, delivers over opportunistic-TLS SMTP, and applies the retry policy.
package delivery

import (
	"context"
	"net"
	"sort"
)

// ResolveMX returns candidate mail hosts for a domain in preference order,
// falling back to the domain's A/AAAA record if no MX exists (RFC 5321 §5.1).
func ResolveMX(ctx context.Context, resolver *net.Resolver, domain string) ([]string, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	mxs, err := resolver.LookupMX(ctx, domain)
	if err == nil && len(mxs) > 0 {
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
	}
	// Implicit MX: fall back to the domain's address records.
	if _, aerr := resolver.LookupHost(ctx, domain); aerr == nil {
		return []string{domain}, nil
	} else if err != nil {
		return nil, err
	} else {
		return nil, aerr
	}
}

func trimDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
