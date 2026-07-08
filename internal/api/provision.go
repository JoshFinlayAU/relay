package api

import (
	"net/http"

	"relay/internal/dns"
	"relay/internal/dnsprovider"
)

type provisionReq struct {
	Provider string `json:"provider"` // "cloudflare"
	APIToken string `json:"api_token"`
}

// handleProvisionDNS auto-creates a domain's DNS records at the provider, then
// verifies. The API token is used transiently and never stored or logged.
func (s *Server) handleProvisionDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	var req provisionReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	if req.Provider != "" && req.Provider != "cloudflare" {
		errBadRequest(w, "unsupported_provider", "only 'cloudflare' is supported")
		return
	}
	key, err := s.Store.GetActiveDKIMKey(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "get dkim key", err)
		return
	}
	specs := dns.PlanRecords(d.Name, d.VerifyToken, key.Selector, key.PublicKey, d.Receiving, s.Params)

	results, err := dnsprovider.ProvisionCloudflare(r.Context(), req.APIToken, d.Name, s.Params.SPFInclude, specs)
	if err != nil {
		errBadRequest(w, "provision_failed", err.Error())
		return
	}
	s.Log.Info("dns provisioned", "domain", d.Name, "records", len(results)) // no token logged
	_ = s.Store.EmitEvent(r.Context(), d.ID, "dns.provisioned", map[string]any{"provider": "cloudflare", "records": len(results)})

	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"note":    "Records created/updated. DNS may take a few minutes to propagate; then verify.",
	})
}
