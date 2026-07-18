package dmarc

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8" ?>
<feedback>
  <report_metadata>
    <org_name>google.com</org_name>
    <email>noreply-dmarc-support@google.com</email>
    <report_id>12345678</report_id>
    <date_range><begin>1700000000</begin><end>1700086400</end></date_range>
  </report_metadata>
  <policy_published>
    <domain>example.com</domain>
    <adkim>r</adkim><aspf>r</aspf>
    <p>quarantine</p><sp>quarantine</sp><pct>100</pct>
  </policy_published>
  <record>
    <row>
      <source_ip>203.0.113.10</source_ip>
      <count>7</count>
      <policy_evaluated><disposition>none</disposition><dkim>pass</dkim><spf>pass</spf></policy_evaluated>
    </row>
    <identifiers><header_from>example.com</header_from></identifiers>
  </record>
  <record>
    <row>
      <source_ip>198.51.100.5</source_ip>
      <count>3</count>
      <policy_evaluated><disposition>quarantine</disposition><dkim>fail</dkim><spf>fail</spf></policy_evaluated>
    </row>
    <identifiers><header_from>example.com</header_from></identifiers>
  </record>
</feedback>`

func TestParseXML(t *testing.T) {
	r, err := ParseXML([]byte(sampleXML))
	if err != nil {
		t.Fatal(err)
	}
	if r.OrgName != "google.com" || r.ReportID != "12345678" || r.Domain != "example.com" {
		t.Errorf("metadata wrong: %+v", r)
	}
	if r.PolicyP != "quarantine" || r.PolicyPct == nil || *r.PolicyPct != 100 {
		t.Errorf("policy wrong: p=%q pct=%v", r.PolicyP, r.PolicyPct)
	}
	if len(r.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(r.Rows))
	}
	if r.Rows[0].SourceIP != "203.0.113.10" || r.Rows[0].Count != 7 || r.Rows[0].DKIM != "pass" {
		t.Errorf("row0 wrong: %+v", r.Rows[0])
	}
	if r.Rows[1].Disposition != "quarantine" || r.Rows[1].SPF != "fail" {
		t.Errorf("row1 wrong: %+v", r.Rows[1])
	}
}

func TestExtractFromEmailGzip(t *testing.T) {
	// gzip the XML and wrap it in a minimal MIME email.
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write([]byte(sampleXML))
	_ = w.Close()

	email := buildMIME("application/gzip", "example.com!report.xml.gz", gz.Bytes())
	reports, err := ExtractFromEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reports))
	}
	if reports[0].Domain != "example.com" || len(reports[0].Rows) != 2 {
		t.Errorf("extracted report wrong: %+v", reports[0])
	}
}

// buildMIME wraps an attachment into a minimal multipart email (base64 body).
func buildMIME(ct, filename string, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString("From: dmarc@reporter.example\r\n")
	b.WriteString("To: dmarc@mail.example.com\r\n")
	b.WriteString("Subject: Report Domain: example.com\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=BOUND\r\n\r\n")
	b.WriteString("--BOUND\r\n")
	b.WriteString("Content-Type: " + ct + "\r\n")
	b.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString(data))
	b.WriteString("\r\n--BOUND--\r\n")
	return b.Bytes()
}
