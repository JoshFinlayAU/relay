package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	miekg "github.com/miekg/dns"

	"relay/internal/dns"
	"relay/internal/store"
)

// dnsInstruction is a single record's publish instruction plus its last check.
type dnsInstruction struct {
	Purpose     string     `json:"purpose"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	Value       string     `json:"value"`
	ZoneLine    string     `json:"zone_line"`
	Required    bool       `json:"required"`
	LastResult  string     `json:"last_result"`
	Observed    string     `json:"observed,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	LastChecked *time.Time `json:"last_checked,omitempty"`
	// SPF-only merge hint when an existing SPF record is present.
	Conflict    bool   `json:"conflict,omitempty"`
	MergedValue string `json:"merged_value,omitempty"`
}

// staticInstructions builds the record set at creation time (no live checks).
func (s *Server) staticInstructions(d store.Domain, selector, dkimPub string) []dnsInstruction {
	specs := dns.PlanRecords(d.Name, d.VerifyToken, selector, dkimPub, d.Receiving, s.Params)
	out := make([]dnsInstruction, 0, len(specs))
	for _, sp := range specs {
		out = append(out, dnsInstruction{
			Purpose:    string(sp.Purpose),
			Type:       sp.Type,
			Name:       sp.Name,
			Value:      sp.Value,
			ZoneLine:   sp.ZoneLine(),
			Required:   sp.Required,
			LastResult: "unknown",
		})
	}
	return out
}

// handleGetDNS returns the publish instructions with stored check results, and
// detects an existing SPF record to offer a merged value + conflict flag.
func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	key, err := s.Store.GetActiveDKIMKey(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "get dkim key", err)
		return
	}
	stored, err := s.Store.ListDNSRecords(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "list dns records", err)
		return
	}
	byPurpose := map[string]store.DnsRecord{}
	for _, rec := range stored {
		byPurpose[rec.Purpose] = rec
	}

	instr := s.staticInstructions(d, key.Selector, key.PublicKey)
	for i := range instr {
		if rec, ok := byPurpose[instr[i].Purpose]; ok {
			instr[i].LastResult = rec.LastResult
			if rec.ObservedValue != nil {
				instr[i].Observed = *rec.ObservedValue
			}
			if rec.Detail != nil {
				instr[i].Detail = *rec.Detail
			}
			instr[i].LastChecked = tsPtr(rec.LastChecked)
		}
		// SPF conflict/merge detection against the live apex TXT.
		if instr[i].Purpose == string(dns.PurposeSPF) {
			if existing := s.currentSPF(r.Context(), d.Name); existing != "" &&
				!strings.Contains(existing, s.Params.SPFInclude) {
				instr[i].Conflict = true
				instr[i].MergedValue = dns.MergeSPF(existing, s.Params.SPFInclude)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"domain":       d.Name,
		"status":       d.Status,
		"instructions": instr,
		"operator_note": fmt.Sprintf(
			"Prerequisite (one-time, operator): publish %s TXT \"v=spf1 ip4:%s ip6:%s -all\" so the customer SPF include resolves.",
			s.Params.SPFInclude, s.SendingIPv4, s.SendingIPv6),
	})
}

// currentSPF returns the domain's existing apex SPF record via a recursive
// resolver, or "" if none/unresolvable.
func (s *Server) currentSPF(ctx context.Context, domain string) string {
	m := new(miekg.Msg)
	m.SetQuestion(miekg.Fqdn(domain), miekg.TypeTXT)
	m.RecursionDesired = true
	c := &miekg.Client{Timeout: 4 * time.Second}
	in, _, err := c.ExchangeContext(ctx, m, "1.1.1.1:53")
	if err != nil {
		return ""
	}
	for _, rr := range in.Answer {
		if t, ok := rr.(*miekg.TXT); ok {
			joined := strings.Join(t.Txt, "")
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(joined)), "v=spf1") {
				return strings.TrimSpace(joined)
			}
		}
	}
	return ""
}

// handleVerifyDomain runs live checks, stores results, and transitions status.
func (s *Server) handleVerifyDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDomain(w, r)
	if !ok {
		return
	}
	key, err := s.Store.GetActiveDKIMKey(r.Context(), d.ID)
	if err != nil {
		errInternal(w, s.Log, "get dkim key", err)
		return
	}

	results, err := s.Verifier.Verify(r.Context(), dns.VerifyInput{
		Domain:     d.Name,
		Token:      d.VerifyToken,
		Selector:   key.Selector,
		DKIMPubB64: key.PublicKey,
		Receiving:  d.Receiving,
	})
	if err != nil {
		errBadRequest(w, "verification_failed", err.Error())
		return
	}

	req := dns.RequiredPurposes(d.Receiving)
	allRequiredPass := true
	for _, res := range results {
		observed := res.Observed
		detail := res.Detail
		_ = s.Store.UpdateDNSRecordResult(r.Context(), store.UpdateDNSRecordResultParams{
			DomainID:      d.ID,
			Purpose:       string(res.Purpose),
			ObservedValue: strPtr(observed),
			LastResult:    string(res.Result),
			Detail:        strPtr(detail),
		})
		// A required record is satisfied by pass OR warn - only a hard fail
		// blocks activation. (SPF over the 10-lookup limit warns: our include is
		// present/effective; the bloat is the customer's to optimise.)
		if req[res.Purpose] && res.Result == dns.ResultFail {
			allRequiredPass = false
		}
	}

	newStatus := d.Status
	switch {
	case allRequiredPass:
		newStatus = "active"
	case d.Status == "active":
		newStatus = "degraded"
	}
	if newStatus != d.Status {
		if d2, err := s.Store.UpdateDomainStatus(r.Context(), store.UpdateDomainStatusParams{ID: d.ID, Status: newStatus}); err == nil {
			d = d2
			_ = s.Store.EmitEvent(r.Context(), d.ID, "domain.status_changed", map[string]any{
				"status": newStatus, "verified": allRequiredPass,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"domain":  toDomainDTO(d),
		"results": results,
		"active":  allRequiredPass,
	})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
