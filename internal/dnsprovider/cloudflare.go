// Package dnsprovider auto-configures a domain's DNS records at a supported
// provider. Cloudflare is supported via a scoped API token (Zone→DNS→Edit).
// The token is used transiently and never stored or logged.
package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"relay/internal/dns"
)

const cfAPI = "https://api.cloudflare.com/client/v4"

// Result is the outcome of provisioning one record.
type Result struct {
	Purpose string `json:"purpose"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Action  string `json:"action"` // created | updated | unchanged | failed
	Error   string `json:"error,omitempty"`
}

type cfClient struct {
	token string
	hc    *http.Client
}

// ProvisionCloudflare creates/updates all planned records for a domain in the
// matching Cloudflare zone. The SPF record is merged with any pre-existing SPF.
func ProvisionCloudflare(ctx context.Context, token, domain, spfInclude string, specs []dns.RecordSpec) ([]Result, error) {
	c := &cfClient{token: strings.TrimSpace(token), hc: &http.Client{Timeout: 20 * time.Second}}
	if c.token == "" {
		return nil, fmt.Errorf("cloudflare api token required")
	}
	zoneID, zoneName, err := c.findZone(ctx, domain)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(specs))
	for _, sp := range specs {
		r := Result{Purpose: string(sp.Purpose), Name: sp.Name, Type: sp.Type}
		value := sp.Value

		// Merge SPF with any existing apex record so we don't create a duplicate.
		if sp.Purpose == dns.PurposeSPF {
			if existing, id := c.findSPF(ctx, zoneID, sp.Name); existing != "" {
				if !strings.Contains(existing, spfInclude) {
					value = dns.MergeSPF(existing, spfInclude)
				} else {
					value = existing
				}
				if err := c.putRecord(ctx, zoneID, id, sp.Type, sp.Name, value, 0); err != nil {
					r.Action, r.Error = "failed", err.Error()
				} else {
					r.Action = "updated"
				}
				results = append(results, r)
				continue
			}
		}

		prio, content := 0, value
		if sp.Type == "MX" {
			prio, content = parseMX(value)
		}
		existingID, existingContent, err := c.findRecord(ctx, zoneID, sp.Type, sp.Name)
		switch {
		case err != nil:
			r.Action, r.Error = "failed", err.Error()
		case existingID == "":
			if err := c.postRecord(ctx, zoneID, sp.Type, sp.Name, content, prio); err != nil {
				r.Action, r.Error = "failed", err.Error()
			} else {
				r.Action = "created"
			}
		case existingContent == content:
			r.Action = "unchanged"
		default:
			if err := c.putRecord(ctx, zoneID, existingID, sp.Type, sp.Name, content, prio); err != nil {
				r.Action, r.Error = "failed", err.Error()
			} else {
				r.Action = "updated"
			}
		}
		results = append(results, r)
	}
	_ = zoneName
	return results, nil
}

// findZone locates the Cloudflare zone for a domain, walking up to the
// registrable domain.
func (c *cfClient) findZone(ctx context.Context, domain string) (id, name string, err error) {
	candidates := []string{domain}
	if base, e := publicsuffix.EffectiveTLDPlusOne(domain); e == nil && base != domain {
		candidates = append(candidates, base)
	}
	for _, cand := range candidates {
		var out struct {
			Success bool `json:"success"`
			Result  []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"result"`
			Errors []cfErr `json:"errors"`
		}
		if err := c.do(ctx, http.MethodGet, cfAPI+"/zones?name="+cand, nil, &out); err != nil {
			return "", "", err
		}
		if len(out.Result) > 0 {
			return out.Result[0].ID, out.Result[0].Name, nil
		}
	}
	return "", "", fmt.Errorf("no Cloudflare zone found for %s in this account (check the token's zone access)", domain)
}

func (c *cfClient) findRecord(ctx context.Context, zoneID, typ, name string) (id, content string, err error) {
	var out struct {
		Result []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"result"`
	}
	url := fmt.Sprintf("%s/zones/%s/dns_records?type=%s&name=%s", cfAPI, zoneID, typ, name)
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return "", "", err
	}
	if len(out.Result) == 0 {
		return "", "", nil
	}
	return out.Result[0].ID, out.Result[0].Content, nil
}

// findSPF returns the existing v=spf1 TXT record (content + id) at name, if any.
func (c *cfClient) findSPF(ctx context.Context, zoneID, name string) (content, id string) {
	var out struct {
		Result []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"result"`
	}
	url := fmt.Sprintf("%s/zones/%s/dns_records?type=TXT&name=%s", cfAPI, zoneID, name)
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return "", ""
	}
	for _, r := range out.Result {
		if strings.HasPrefix(strings.ToLower(strings.Trim(r.Content, "\"")), "v=spf1") {
			return strings.Trim(r.Content, "\""), r.ID
		}
	}
	return "", ""
}

type cfRecordBody struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority,omitempty"`
}

func (c *cfClient) postRecord(ctx context.Context, zoneID, typ, name, content string, prio int) error {
	return c.writeRecord(ctx, http.MethodPost, cfAPI+"/zones/"+zoneID+"/dns_records", typ, name, content, prio)
}

func (c *cfClient) putRecord(ctx context.Context, zoneID, recID, typ, name, content string, prio int) error {
	return c.writeRecord(ctx, http.MethodPut, cfAPI+"/zones/"+zoneID+"/dns_records/"+recID, typ, name, content, prio)
}

func (c *cfClient) writeRecord(ctx context.Context, method, url, typ, name, content string, prio int) error {
	body := cfRecordBody{Type: typ, Name: name, Content: content, TTL: 300}
	if typ == "MX" {
		body.Priority = &prio
	}
	var out struct {
		Success bool    `json:"success"`
		Errors  []cfErr `json:"errors"`
	}
	if err := c.do(ctx, method, url, body, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("%s", cfErrs(out.Errors))
	}
	return nil
}

type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func cfErrs(errs []cfErr) string {
	if len(errs) == 0 {
		return "cloudflare API error"
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "; ")
}

func (c *cfClient) do(ctx context.Context, method, url string, body, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("cloudflare rejected the token (check DNS:Edit permission for the zone)")
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// parseMX splits "10 mail.example.com." into priority and host.
func parseMX(v string) (int, string) {
	fields := strings.Fields(v)
	if len(fields) == 2 {
		if p, err := strconv.Atoi(fields[0]); err == nil {
			return p, strings.TrimSuffix(fields[1], ".")
		}
	}
	return 10, strings.TrimSuffix(v, ".")
}
