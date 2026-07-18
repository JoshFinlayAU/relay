package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"relay/internal/dmarc"
	"relay/internal/storage"
)

func reportEmail(domain string) []byte {
	end := time.Now().Unix()
	begin := end - 86400
	xml := fmt.Sprintf(`<?xml version="1.0"?>
<feedback><report_metadata><org_name>google.com</org_name><report_id>rpt-%s</report_id>
<date_range><begin>%d</begin><end>%d</end></date_range></report_metadata>
<policy_published><domain>%s</domain><p>none</p><pct>100</pct></policy_published>
<record><row><source_ip>203.0.113.9</source_ip><count>10</count>
<policy_evaluated><disposition>none</disposition><dkim>pass</dkim><spf>pass</spf></policy_evaluated></row>
<identifiers><header_from>%s</header_from></identifiers></record>
<record><row><source_ip>198.51.100.7</source_ip><count>4</count>
<policy_evaluated><disposition>quarantine</disposition><dkim>fail</dkim><spf>fail</spf></policy_evaluated></row>
<identifiers><header_from>%s</header_from></identifiers></record></feedback>`, domain, begin, end, domain, domain, domain)

	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write([]byte(xml))
	_ = w.Close()

	var b bytes.Buffer
	b.WriteString("From: dmarc@google.com\r\nTo: dmarc@mail.test\r\nSubject: Report\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\n")
	b.WriteString("Content-Type: application/gzip\r\nContent-Disposition: attachment; filename=\"r.xml.gz\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString(gz.Bytes()))
	b.WriteString("\r\n--B--\r\n")
	return b.Bytes()
}

func TestDMARCIngestAndAnalyze(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	did := createDomainForTest(t, ts.URL, testToken, "dmarctest.example")

	blobs, _ := storage.New(t.TempDir())
	ing := &dmarc.Ingester{Store: testStore, Blobs: blobs, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	n, err := ing.Ingest(ctx, reportEmail("dmarctest.example"))
	if err != nil || n != 1 {
		t.Fatalf("ingest = %d, %v; want 1, nil", n, err)
	}
	// Idempotent: same report doesn't double-count.
	if n2, _ := ing.Ingest(ctx, reportEmail("dmarctest.example")); n2 != 0 {
		t.Errorf("re-ingest stored %d, want 0 (dedupe)", n2)
	}

	status, out := do(t, "GET", ts.URL+"/v1/domains/"+did+"/dmarc?window=30d", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("dmarc analyzer = %d (%v)", status, out)
	}
	sum := out["summary"].(map[string]any)
	if sum["total"].(float64) != 14 || sum["passed"].(float64) != 10 || sum["quarantined"].(float64) != 4 {
		t.Errorf("summary wrong: %v", sum)
	}
	if srcs := out["top_sources"].([]any); len(srcs) != 2 {
		t.Errorf("top_sources = %d, want 2", len(srcs))
	}
	if reps := out["reports"].([]any); len(reps) != 1 {
		t.Errorf("reports = %d, want 1", len(reps))
	}
}
