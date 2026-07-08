package api

import (
	"net/http"
	"time"
)

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
	})
}
