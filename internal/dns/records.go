// Package dns generates the DNS records a domain must publish and verifies them
// against the domain's authoritative nameservers.
package dns

import (
	"fmt"
	"strings"

	"relay/internal/dkim"
)

// Purpose identifies a record's role in onboarding.
type Purpose string

const (
	PurposeOwnership Purpose = "ownership"
	PurposeDKIM      Purpose = "dkim"
	PurposeSPF       Purpose = "spf"
	PurposeDMARC     Purpose = "dmarc"
	PurposeBounceMX  Purpose = "bounce_mx"
	PurposeBounceSPF Purpose = "bounce_spf"
	PurposeInboundMX Purpose = "inbound_mx"
)

// RecordSpec is a single DNS record the customer must publish.
type RecordSpec struct {
	Purpose  Purpose `json:"purpose"`
	Type     string  `json:"type"`  // TXT | MX
	Name     string  `json:"name"`  // FQDN
	Value    string  `json:"value"` // record data (TXT string, or "pref host." for MX)
	Required bool    `json:"required"`
}

// Params carries the server-wide values needed to build a domain's records.
type Params struct {
	Hostname   string // server FQDN, used as MX target and EHLO name
	SPFInclude string // include: target for customer SPF
	DMARCRua   string // DMARC rua mailto
}

// BounceSubdomain returns the bounce subdomain for a domain.
func BounceSubdomain(domain string) string { return "bounce." + domain }

// OwnershipName returns the ownership-verification record name.
func OwnershipName(domain string) string { return "_relay-verify." + domain }

// OwnershipValue returns the ownership TXT value for a token.
func OwnershipValue(token string) string { return "relay-verify=" + token }

// DKIMName returns the DKIM record name for a selector.
func DKIMName(selector, domain string) string {
	return selector + "._domainkey." + domain
}

// PlanRecords produces the full record set for a domain.
func PlanRecords(domain, token, selector, dkimPublicB64 string, receiving bool, p Params) []RecordSpec {
	recs := []RecordSpec{
		{
			Purpose:  PurposeOwnership,
			Type:     "TXT",
			Name:     OwnershipName(domain),
			Value:    OwnershipValue(token),
			Required: true,
		},
		{
			Purpose:  PurposeDKIM,
			Type:     "TXT",
			Name:     DKIMName(selector, domain),
			Value:    dkim.TXTValue(dkimPublicB64),
			Required: true,
		},
		{
			Purpose:  PurposeSPF,
			Type:     "TXT",
			Name:     domain,
			Value:    fmt.Sprintf("v=spf1 include:%s ~all", p.SPFInclude),
			Required: true,
		},
		{
			Purpose:  PurposeDMARC,
			Type:     "TXT",
			Name:     "_dmarc." + domain,
			Value:    fmt.Sprintf("v=DMARC1; p=none; rua=%s", p.DMARCRua),
			Required: false, // recommended, not blocking for activation
		},
		{
			Purpose:  PurposeBounceMX,
			Type:     "MX",
			Name:     BounceSubdomain(domain),
			Value:    fmt.Sprintf("10 %s.", p.Hostname),
			Required: true,
		},
		{
			// Recommended: SPF on the bounce subdomain so the VERP envelope also
			// yields spf=pass (DMARC already passes via DKIM alignment).
			Purpose:  PurposeBounceSPF,
			Type:     "TXT",
			Name:     BounceSubdomain(domain),
			Value:    fmt.Sprintf("v=spf1 include:%s -all", p.SPFInclude),
			Required: false,
		},
	}
	if receiving {
		recs = append(recs, RecordSpec{
			Purpose:  PurposeInboundMX,
			Type:     "MX",
			Name:     domain,
			Value:    fmt.Sprintf("10 %s.", p.Hostname),
			Required: true,
		})
	}
	return recs
}

// MergeSPF inserts our include before the trailing "all" mechanism of an
// existing SPF record, so a domain with pre-existing SPF gets a single merged
// record rather than a second (invalid) one.
func MergeSPF(existing, include string) string {
	inc := "include:" + include
	var head []string
	var tail string
	for _, f := range strings.Fields(existing) {
		switch strings.ToLower(f) {
		case "~all", "-all", "?all", "+all":
			tail = f
		default:
			head = append(head, f)
		}
	}
	head = append(head, inc)
	if tail != "" {
		head = append(head, tail)
	}
	return strings.Join(head, " ")
}

// ZoneLine renders a copy-paste BIND zone-file line for a record.
func (r RecordSpec) ZoneLine() string {
	switch r.Type {
	case "TXT":
		return fmt.Sprintf("%s. IN TXT \"%s\"", r.Name, r.Value)
	case "MX":
		return fmt.Sprintf("%s. IN MX %s", r.Name, r.Value)
	default:
		return fmt.Sprintf("%s. IN %s %s", r.Name, r.Type, r.Value)
	}
}
