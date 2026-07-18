package api

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"

	"github.com/google/uuid"

	"relay/internal/store"
)

type tlsCertReq struct {
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`
}

// parsedCert validates a cert+key pair and extracts the leaf details.
type parsedCert struct {
	subjects  []string
	notBefore time.Time
	notAfter  time.Time
}

func validateCertKey(certPEM, keyPEM string) (*parsedCert, error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	subs := append([]string{}, leaf.DNSNames...)
	if leaf.Subject.CommonName != "" && !contains(subs, leaf.Subject.CommonName) {
		subs = append(subs, leaf.Subject.CommonName)
	}
	return &parsedCert{subjects: subs, notBefore: leaf.NotBefore, notAfter: leaf.NotAfter}, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// --- server-hostname cert (config-file based) status + hot reload ---

// handleGetServerTLS reports how the server-hostname cert is sourced.
func (s *Server) handleGetServerTLS(w http.ResponseWriter, r *http.Request) {
	source := s.TLSSource
	if source == "" {
		source = "disabled"
	}
	out := map[string]any{"source": source}
	if s.CertExpiry != nil {
		if na, ok := s.CertExpiry(); ok {
			out["not_after"] = na.UTC()
			out["days_remaining"] = int(time.Until(na).Hours() / 24)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleReloadServerTLS hot-reloads certs from disk/DB (swap files then call
// this to pick up renewed certs with no restart / downtime).
func (s *Server) handleReloadServerTLS(w http.ResponseWriter, r *http.Request) {
	if s.CertStore == nil {
		errBadRequest(w, "tls_disabled", "TLS is not enabled")
		return
	}
	if err := s.CertStore.Reload(r.Context()); err != nil {
		errInternal(w, s.Log, "reload tls certs", err)
		return
	}
	_ = s.Store.EmitEvent(r.Context(), uuid.Nil, "tls.reloaded", nil)
	s.handleGetServerTLS(w, r)
}

// --- per-domain certs ---

func (s *Server) handleGetDomainTLS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	c, err := s.Store.GetDomainTLSCert(r.Context(), d.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"subjects":   c.Subjects,
		"not_before": tsPtr(c.NotBefore),
		"not_after":  tsPtr(c.NotAfter),
		"updated_at": tsPtr(c.UpdatedAt),
	})
}

func (s *Server) handlePutDomainTLS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	var req tlsCertReq
	if err := decodeJSON(r, &req); err != nil {
		errBadRequest(w, "invalid_json", err.Error())
		return
	}
	pc, err := validateCertKey(req.CertPEM, req.KeyPEM)
	if err != nil {
		errBadRequest(w, "invalid_certificate", "certificate/key invalid: "+err.Error())
		return
	}
	keyEnc, err := s.Sealer.Seal([]byte(req.KeyPEM))
	if err != nil {
		errInternal(w, s.Log, "seal tls key", err)
		return
	}
	if _, err := s.Store.UpsertDomainTLSCert(r.Context(), store.UpsertDomainTLSCertParams{
		DomainID: d.ID, CertPem: req.CertPEM, KeyEnc: keyEnc, Subjects: pc.subjects,
		NotBefore: tsFrom(pc.notBefore), NotAfter: tsFrom(pc.notAfter),
	}); err != nil {
		errInternal(w, s.Log, "store tls cert", err)
		return
	}
	s.reloadCerts(r)
	_ = s.Store.EmitEvent(r.Context(), d.ID, "tls.domain_cert_updated", map[string]any{"subjects": pc.subjects})
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true, "subjects": pc.subjects,
		"not_before": pc.notBefore.UTC(), "not_after": pc.notAfter.UTC(),
	})
}

func (s *Server) handleDeleteDomainTLS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	n, err := s.Store.DeleteDomainTLSCert(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "delete tls cert", err)
		return
	}
	if n == 0 {
		errNotFound(w, "no certificate for this domain")
		return
	}
	s.reloadCerts(r)
	_ = s.Store.EmitEvent(r.Context(), d.ID, "tls.domain_cert_removed", nil)
	w.WriteHeader(http.StatusNoContent)
}

// reloadCerts refreshes the in-memory CertStore after a DB change (nil in tests).
func (s *Server) reloadCerts(r *http.Request) {
	if s.CertStore != nil {
		if err := s.CertStore.Reload(r.Context()); err != nil {
			s.Log.Warn("tls: reload after change", "err", err)
		}
	}
}
