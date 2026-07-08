package webhook

import (
	"encoding/base64"
	"strings"
	"testing"
)

var mimeMsg = "From: Alice <alice@example.com>\r\n" +
	"To: support@voxteam.app\r\n" +
	"Subject: Help please\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=BND\r\n" +
	"\r\n" +
	"--BND\r\n" +
	"Content-Type: text/plain\r\n\r\nHello, this is the text body.\r\n" +
	"--BND\r\n" +
	"Content-Type: text/html\r\n\r\n<p>Hello HTML</p>\r\n" +
	"--BND\r\n" +
	"Content-Type: application/pdf\r\n" +
	"Content-Disposition: attachment; filename=\"invoice.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\n" +
	base64.StdEncoding.EncodeToString([]byte("PDFDATA")) + "\r\n" +
	"--BND--\r\n"

func TestBuildPayload(t *testing.T) {
	p := BuildPayload([]byte(mimeMsg), nil)
	if p.From != "alice@example.com" {
		t.Errorf("from = %q", p.From)
	}
	if len(p.To) != 1 || p.To[0] != "support@voxteam.app" {
		t.Errorf("to = %v", p.To)
	}
	if p.Subject != "Help please" {
		t.Errorf("subject = %q", p.Subject)
	}
	if !strings.Contains(p.Text, "text body") {
		t.Errorf("text = %q", p.Text)
	}
	if !strings.Contains(p.HTML, "Hello HTML") {
		t.Errorf("html = %q", p.HTML)
	}
	if len(p.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(p.Attachments))
	}
	att := p.Attachments[0]
	if att.Filename != "invoice.pdf" || att.ContentType != "application/pdf" {
		t.Errorf("attachment meta wrong: %+v", att)
	}
	decoded, _ := base64.StdEncoding.DecodeString(att.Content)
	if string(decoded) != "PDFDATA" {
		t.Errorf("attachment content = %q", decoded)
	}
}

func TestSignAndVerify(t *testing.T) {
	secret := []byte("s3cr3t")
	body := []byte(`{"hello":"world"}`)
	ts := "1700000000"
	sig := "sha256=" + signBody(secret, ts, body)
	if !Verify(secret, ts, body, sig) {
		t.Error("valid signature failed to verify")
	}
	if Verify([]byte("wrong"), ts, body, sig) {
		t.Error("wrong secret verified")
	}
	if Verify(secret, ts, []byte("tampered"), sig) {
		t.Error("tampered body verified")
	}
}
