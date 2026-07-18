// Package api wires the chi HTTP router: REST under /v1, health, metrics, and
// the embedded SPA.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"relay/internal/certs"
	"relay/internal/crypto"
	"relay/internal/dns"
	"relay/internal/storage"
	"relay/internal/store"
)

// Server holds dependencies shared by handlers.
type Server struct {
	Store       *store.Store
	Log         *slog.Logger
	Dist        fs.FS
	Tokens      []string
	Sealer      *crypto.Sealer
	Verifier    *dns.Verifier
	Params      dns.Params
	SendingIPv4 string
	SendingIPv6 string
	Blobs       *storage.Store
	Hostname    string

	// Settings/server-info.
	Version       string
	TLSEnabled    bool
	ListenerAddrs map[string]string
	CertExpiry    func() (time.Time, bool) // nil when no managed cert

	// MetricsAddr, when non-empty, means Prometheus /metrics is served on a
	// separate listener (bound in main) and must NOT be exposed on the public
	// mux. When empty, /metrics is mounted on the public mux behind admin auth.
	MetricsAddr string

	// Retention defaults from static config, reported by the settings endpoint
	// when no runtime policy has been saved.
	RetentionDefaultDays    int
	RetentionDefaultEnabled bool

	// TLS: manual/per-domain cert store (nil when TLS is off) and how the server
	// cert is sourced ("acme" | "manual-file" | "self-signed" | "disabled").
	CertStore *certs.CertStore
	TLSSource string
}

// MetricsHandler is the Prometheus handler, exported so main can mount it on a
// separately-bound listener when MetricsAddr is set.
func MetricsHandler() http.Handler { return promhttp.Handler() }

// Router builds the top-level HTTP handler.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// NB: no middleware.RealIP - relayd binds directly to its public IP with no
	// trusted proxy, so r.RemoteAddr is authoritative. Trusting X-Forwarded-For
	// here would let clients spoof the source IP used for AUTH rate-limiting.
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Operational endpoints.
	r.Get("/healthz", s.handleHealth) // no auth (liveness), but returns no internals
	// /metrics: served on a separate bound listener when MetricsAddr is set;
	// otherwise mounted here but gated behind admin auth so volumes/queue depth
	// aren't world-readable (spec: "bound separately or IP-restricted").
	if s.MetricsAddr == "" {
		r.With(s.requireAuth).Handle("/metrics", promhttp.Handler())
	}

	// Versioned API.
	r.Route("/v1", func(r chi.Router) {
		r.Get("/ping", s.handlePing)
		r.Post("/auth/login", s.handleLogin) // unauthenticated

		// Authenticated endpoints.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/auth/verify", s.handleAuthVerify)
			r.Post("/auth/logout", s.handleLogout)

			// Admin user management.
			r.Get("/admin/users", s.handleListAdminUsers)
			r.Post("/admin/users", s.handleCreateAdminUser)
			r.Post("/admin/users/{id}/password", s.handleChangeAdminPassword)
			r.Delete("/admin/users/{id}", s.handleDeleteAdminUser)

			// API keys (programmatic access; secret shown once).
			r.Get("/api-keys", s.handleListAPIKeys)
			r.Post("/api-keys", s.handleCreateAPIKey)
			r.Delete("/api-keys/{id}", s.handleRevokeAPIKey)

			r.Post("/domains", s.handleCreateDomain)
			r.Get("/domains", s.handleListDomains)
			r.Get("/domains/{id}", s.handleGetDomain)
			r.Delete("/domains/{id}", s.handleDeleteDomain)
			r.Get("/domains/{id}/dns", s.handleGetDNS)
			r.Post("/domains/{id}/dns/provision", s.handleProvisionDNS)
			r.Post("/domains/{id}/verify", s.handleVerifyDomain)
			r.Patch("/domains/{id}", s.handlePatchDomain)
			r.Get("/domains/{id}/stats", s.handleDomainStats)
			r.Get("/domains/{id}/stats/timeseries", s.handleDomainTimeseries)
			r.Get("/domains/{id}/dmarc", s.handleDomainDMARC)
			r.Post("/domains/{id}/test-send", s.handleTestSend)

			// Credentials.
			r.Post("/domains/{id}/credentials", s.handleCreateCredential)
			r.Get("/domains/{id}/credentials", s.handleListCredentials)
			r.Get("/credentials/{id}", s.handleGetCredential)
			r.Patch("/credentials/{id}", s.handlePatchCredential)
			r.Delete("/credentials/{id}", s.handleDeleteCredential)
			r.Get("/credentials/{id}/stats", s.handleCredentialStats)

			// Mailboxes & webhooks.
			r.Post("/domains/{id}/mailboxes", s.handleCreateMailbox)
			r.Get("/domains/{id}/mailboxes", s.handleListMailboxes)
			r.Patch("/mailboxes/{id}", s.handlePatchMailbox)
			r.Delete("/mailboxes/{id}", s.handleDeleteMailbox)
			r.Get("/domains/{id}/webhook-deliveries", s.handleListWebhookDeliveries)
			r.Post("/webhook-deliveries/{id}/redeliver", s.handleRedeliverWebhook)

			// Suppressions.
			r.Get("/domains/{id}/suppressions", s.handleListSuppressions)
			r.Post("/domains/{id}/suppressions", s.handleAddSuppression)
			r.Delete("/domains/{id}/suppressions", s.handleRemoveSuppression)

			// Messages + stats.
			r.Get("/messages", s.handleListMessages)
			r.Get("/messages/{id}", s.handleGetMessage)
			r.Get("/messages/{id}/raw", s.handleGetMessageRaw)
			r.Get("/stats/overview", s.handleStatsOverview)

			// Events + settings.
			r.Get("/events", s.handleListEvents)
			r.Get("/server/info", s.handleServerInfo)
			r.Get("/settings/retention", s.handleGetRetention)
			r.Put("/settings/retention", s.handleSetRetention)
			// TLS: server-hostname cert status + hot reload; per-domain certs.
			r.Get("/settings/tls", s.handleGetServerTLS)
			r.Post("/settings/tls/reload", s.handleReloadServerTLS)
			r.Get("/domains/{id}/tls-cert", s.handleGetDomainTLS)
			r.Put("/domains/{id}/tls-cert", s.handlePutDomainTLS)
			r.Delete("/domains/{id}/tls-cert", s.handleDeleteDomainTLS)
		})
	})

	// SPA (must be last; catches everything else).
	if s.Dist != nil {
		r.NotFound(spaHandler(s.Dist).ServeHTTP)
		r.Get("/", spaHandler(s.Dist).ServeHTTP)
	}
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		// Don't leak the raw DB error string on an unauthenticated endpoint;
		// log the detail and return a generic status.
		s.Log.Warn("healthz db ping failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unhealthy", "db": "unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "db": "ok"})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"pong": true})
}

// handleAuthVerify lets the WebUI confirm a token is valid at login.
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
