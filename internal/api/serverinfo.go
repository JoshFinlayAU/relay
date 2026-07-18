package api

import (
	"net/http"
	"strings"
	"time"
)

// serverDNSRecords are the records the operator must publish for the mail host
// itself (distinct from per-customer-domain records): forward A/AAAA and the
// SPF include target that authorises the sending IPs.
func (s *Server) serverDNSRecords() []map[string]string {
	recs := []map[string]string{}
	if s.SendingIPv4 != "" {
		recs = append(recs, map[string]string{"purpose": "host_a", "type": "A", "name": s.Hostname, "value": s.SendingIPv4})
	}
	if s.SendingIPv6 != "" {
		recs = append(recs, map[string]string{"purpose": "host_aaaa", "type": "AAAA", "name": s.Hostname, "value": s.SendingIPv6})
	}
	// SPF include target: v=spf1 with whichever IPs we have.
	var mech []string
	if s.SendingIPv4 != "" {
		mech = append(mech, "ip4:"+s.SendingIPv4)
	}
	if s.SendingIPv6 != "" {
		mech = append(mech, "ip6:"+s.SendingIPv6)
	}
	if s.Params.SPFInclude != "" && len(mech) > 0 {
		recs = append(recs, map[string]string{
			"purpose": "spf_target", "type": "TXT", "name": s.Params.SPFInclude,
			"value": "v=spf1 " + strings.Join(mech, " ") + " -all",
		})
	}
	// DMARC external-destination authorisation: lets any domain send its
	// aggregate reports to dmarc@<hostname> (RFC 7489 §7.1). Wildcard covers all.
	recs = append(recs, map[string]string{
		"purpose": "dmarc_report_auth", "type": "TXT",
		"name": "*._report._dmarc." + s.Hostname, "value": "v=DMARC1",
	})
	return recs
}

// handleServerInfo powers the Settings screen: identity, TLS/cert status,
// listener addresses, live queue depth, and DB health.
func (s *Server) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	dbOK := s.Store.Ping(r.Context()) == nil
	depth, _ := s.Store.QueueDepth(r.Context())

	cert := map[string]any{"managed": s.TLSEnabled}
	if s.CertExpiry != nil {
		if notAfter, ok := s.CertExpiry(); ok {
			cert["not_after"] = notAfter.UTC()
			cert["days_remaining"] = int(time.Until(notAfter).Hours() / 24)
		}
	}

	version := s.Version
	if version == "" {
		version = "dev"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hostname":     s.Hostname,
		"version":      version,
		"tls_enabled":  s.TLSEnabled,
		"listeners":    s.ListenerAddrs,
		"queue_depth":  depth,
		"database":     map[string]bool{"ok": dbOK},
		"cert":         cert,
		"sending_ipv4": s.SendingIPv4,
		"sending_ipv6": s.SendingIPv6,
		"spf_include":  s.Params.SPFInclude,
		"dmarc_rua":    s.Params.DMARCRua,
		"ptr_expected": s.Hostname, // reverse DNS for the sending IPs must resolve here
		"server_dns":   s.serverDNSRecords(),
	})
}
