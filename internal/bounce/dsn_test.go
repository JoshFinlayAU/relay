package bounce

import (
	"strings"
	"testing"
)

const hardDSN = `From: MAILER-DAEMON@mx.example.com
To: bounce-x@bounce.voxteam.app
Subject: Undelivered Mail Returned to Sender
Content-Type: multipart/report; report-type=delivery-status; boundary="BOUND"

--BOUND
Content-Type: text/plain

Delivery failed permanently.

--BOUND
Content-Type: message/delivery-status

Reporting-MTA: dns; mx.example.com

Final-Recipient: rfc822; nobody@example.net
Action: failed
Status: 5.1.1
Diagnostic-Code: smtp; 550 5.1.1 user unknown

--BOUND--
`

const softDSN = `From: MAILER-DAEMON@mx.example.com
To: bounce-x@bounce.voxteam.app
Content-Type: multipart/report; report-type=delivery-status; boundary="B2"

--B2
Content-Type: message/delivery-status

Final-Recipient: rfc822; busy@example.net
Action: delayed
Status: 4.2.2
Diagnostic-Code: smtp; 452 4.2.2 mailbox full

--B2--
`

const arf = `From: complaints@isp.example
To: bounce-x@bounce.voxteam.app
Content-Type: multipart/report; report-type=feedback-report; boundary="ARF"

--ARF
Content-Type: text/plain

This is a spam complaint.

--ARF
Content-Type: message/feedback-report

Feedback-Type: abuse
User-Agent: SomeISP!1.0

--ARF--
`

func TestParseHardDSN(t *testing.T) {
	r := Parse([]byte(hardDSN))
	if r.Type != TypeHard {
		t.Errorf("type = %s, want hard", r.Type)
	}
	if r.Status != "5.1.1" {
		t.Errorf("status = %q", r.Status)
	}
	if r.Recipient != "nobody@example.net" {
		t.Errorf("recipient = %q", r.Recipient)
	}
}

func TestParseSoftDSN(t *testing.T) {
	r := Parse([]byte(softDSN))
	if r.Type != TypeSoft {
		t.Errorf("type = %s, want soft (status %q)", r.Type, r.Status)
	}
}

func TestParseComplaint(t *testing.T) {
	r := Parse([]byte(arf))
	if r.Type != TypeComplaint {
		t.Errorf("type = %s, want complaint", r.Type)
	}
}

func TestHeuristicNonConformant(t *testing.T) {
	// A server that sends prose instead of a DSN.
	raw := "From: postmaster@mx.example\r\nSubject: failure\r\n\r\nSorry, the recipient's mailbox is full, try again later.\r\n"
	r := Parse([]byte(raw))
	if r.Type != TypeSoft {
		t.Errorf("type = %s, want soft (heuristic)", r.Type)
	}

	raw2 := "From: postmaster@mx.example\r\n\r\n550 user unknown; no such user here\r\n"
	if r2 := Parse([]byte(raw2)); r2.Type != TypeHard {
		t.Errorf("type = %s, want hard (heuristic)", r2.Type)
	}
}

func TestHeuristicStatusCode(t *testing.T) {
	if r := Parse([]byte("blah 5.7.1 blah")); r.Type != TypeHard || r.Status != "5.7.1" {
		t.Errorf("got %+v", r)
	}
}

func TestParseGarbage(t *testing.T) {
	if r := Parse([]byte("totally unparseable nonsense")); r.Type != TypeUnknown {
		t.Errorf("type = %s, want unknown", r.Type)
	}
	_ = strings.TrimSpace("")
}
