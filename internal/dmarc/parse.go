// Package dmarc parses DMARC aggregate (RUA) reports: it pulls the report XML
// out of an email (gzip/zip/xml), decodes it, and exposes a flat structure for
// storage and analysis.
package dmarc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/emersion/go-message/mail"
)

// Report is a decoded DMARC aggregate report.
type Report struct {
	OrgName   string
	ReportID  string
	Begin     time.Time
	End       time.Time
	Domain    string // policy_published.domain (the reported-on domain)
	PolicyP   string // published policy: none|quarantine|reject
	PolicyPct *int32
	Rows      []Row
}

// Row is one evaluated source within a report.
type Row struct {
	SourceIP    string
	Count       int32
	Disposition string // none|quarantine|reject
	DKIM        string // aligned DKIM result: pass|fail|none
	SPF         string // aligned SPF result
	HeaderFrom  string
}

// feedback mirrors the RFC 7489 aggregate report schema (subset).
type feedback struct {
	XMLName  xml.Name `xml:"feedback"`
	Metadata struct {
		OrgName   string `xml:"org_name"`
		ReportID  string `xml:"report_id"`
		DateRange struct {
			Begin int64 `xml:"begin"`
			End   int64 `xml:"end"`
		} `xml:"date_range"`
	} `xml:"report_metadata"`
	PolicyPublished struct {
		Domain string `xml:"domain"`
		P      string `xml:"p"`
		Pct    string `xml:"pct"`
	} `xml:"policy_published"`
	Records []struct {
		Row struct {
			SourceIP        string `xml:"source_ip"`
			Count           int32  `xml:"count"`
			PolicyEvaluated struct {
				Disposition string `xml:"disposition"`
				DKIM        string `xml:"dkim"`
				SPF         string `xml:"spf"`
			} `xml:"policy_evaluated"`
		} `xml:"row"`
		Identifiers struct {
			HeaderFrom string `xml:"header_from"`
		} `xml:"identifiers"`
	} `xml:"record"`
}

// ParseXML decodes a raw aggregate-report XML document.
func ParseXML(data []byte) (*Report, error) {
	var f feedback
	if err := xml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("dmarc xml: %w", err)
	}
	if f.Metadata.ReportID == "" || f.PolicyPublished.Domain == "" {
		return nil, fmt.Errorf("dmarc xml: missing report_id or policy domain")
	}
	r := &Report{
		OrgName:  f.Metadata.OrgName,
		ReportID: f.Metadata.ReportID,
		Begin:    time.Unix(f.Metadata.DateRange.Begin, 0).UTC(),
		End:      time.Unix(f.Metadata.DateRange.End, 0).UTC(),
		Domain:   f.PolicyPublished.Domain,
		PolicyP:  f.PolicyPublished.P,
	}
	if pct, err := strconv.Atoi(f.PolicyPublished.Pct); err == nil {
		p := int32(pct)
		r.PolicyPct = &p
	}
	for _, rec := range f.Records {
		r.Rows = append(r.Rows, Row{
			SourceIP:    rec.Row.SourceIP,
			Count:       rec.Row.Count,
			Disposition: orDefault(rec.Row.PolicyEvaluated.Disposition, "none"),
			DKIM:        orDefault(rec.Row.PolicyEvaluated.DKIM, "none"),
			SPF:         orDefault(rec.Row.PolicyEvaluated.SPF, "none"),
			HeaderFrom:  rec.Identifiers.HeaderFrom,
		})
	}
	return r, nil
}

// ExtractFromEmail walks an inbound email and returns every DMARC report found
// in its parts (gzip / zip / bare XML). Parsing failures on individual parts are
// skipped so one bad attachment doesn't drop the rest.
func ExtractFromEmail(raw []byte) ([]*Report, error) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// Not MIME — maybe the whole body is a report.
		if rep, perr := decodeReport(raw); perr == nil {
			return []*Report{rep}, nil
		}
		return nil, err
	}
	var reports []*Report
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		body, _ := io.ReadAll(part.Body)
		reports = append(reports, decodeReports(body)...)
	}
	return reports, nil
}

// decodeReports turns one part's bytes into 0..n reports (a zip may hold many).
func decodeReports(b []byte) []*Report {
	switch {
	case len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b: // gzip
		if zr, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			if dec, err := io.ReadAll(zr); err == nil {
				if rep, err := ParseXML(dec); err == nil {
					return []*Report{rep}
				}
			}
		}
	case len(b) >= 4 && b[0] == 'P' && b[1] == 'K': // zip
		return reportsFromZip(b)
	default:
		if rep, err := decodeReport(b); err == nil {
			return []*Report{rep}
		}
	}
	return nil
}

func reportsFromZip(b []byte) []*Report {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil
	}
	var out []*Report
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		if rep, err := ParseXML(data); err == nil {
			out = append(out, rep)
		}
	}
	return out
}

// decodeReport tries bare XML (already decompressed).
func decodeReport(b []byte) (*Report, error) {
	return ParseXML(b)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
