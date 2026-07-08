package submission

import (
	"strings"
	"testing"
	"time"
)

func TestAssembleStripsReceivedAndInjects(t *testing.T) {
	raw := "Received: from evil.internal [10.0.0.5] by app\r\n" +
		"From: app@voxsub.example\r\n" +
		"To: dest@example.net\r\n" +
		"Subject: Hi\r\n" +
		"\r\n" +
		"body line\r\n"
	out := string(assemble([]byte(raw), assembleOptions{
		receivedHeader: "from [1.2.3.4] by mail.test with ESMTPA id X; now",
		messageID:      "<abc@mail.test>",
		date:           time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
	}))

	if strings.Contains(out, "10.0.0.5") {
		t.Error("inbound Received (leaking app IP) was not stripped")
	}
	if !strings.Contains(out, "Received: from [1.2.3.4] by mail.test") {
		t.Error("our Received header missing")
	}
	if !strings.Contains(out, "Message-ID: <abc@mail.test>") {
		t.Error("Message-ID not injected")
	}
	if !strings.Contains(out, "Date: ") {
		t.Error("Date not injected")
	}
	if !strings.Contains(out, "From: app@voxsub.example") {
		t.Error("From header lost")
	}
	// Our Received must be first.
	if !strings.HasPrefix(out, "Received: from [1.2.3.4]") {
		t.Error("our Received should be the first header")
	}
}

func TestAssembleKeepsExistingMessageIDAndDate(t *testing.T) {
	raw := "From: app@voxsub.example\r\nMessage-ID: <orig@app>\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nbody\r\n"
	out := string(assemble([]byte(raw), assembleOptions{
		receivedHeader: "r", messageID: "<new@mail.test>", date: time.Now(),
	}))
	if strings.Contains(out, "<new@mail.test>") {
		t.Error("should not overwrite an existing Message-ID")
	}
	if !strings.Contains(out, "<orig@app>") {
		t.Error("original Message-ID lost")
	}
	if strings.Count(out, "Date:") != 1 {
		t.Errorf("expected exactly one Date header, got %d", strings.Count(out, "Date:"))
	}
}

func TestHeaderFromParsing(t *testing.T) {
	raw := []byte("From: \"App Name\" <app@voxsub.example>\r\nTo: x@y.com\r\n\r\nhi\r\n")
	got, err := headerFrom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "app@voxsub.example" {
		t.Errorf("headerFrom = %q", got)
	}
	if domainOf(got) != "voxsub.example" {
		t.Errorf("domainOf = %q", domainOf(got))
	}
}

func TestSanitizeHeaderStripsCRLF(t *testing.T) {
	got := sanitizeHeader("evil\r\nBcc: attacker@x.com\x00\tok")
	if strings.ContainsAny(got, "\r\n\x00") {
		t.Errorf("control chars survived: %q", got)
	}
	if got != "evilBcc: attacker@x.comok" {
		t.Errorf("sanitizeHeader = %q", got)
	}
}
