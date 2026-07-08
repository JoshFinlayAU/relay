package bounce

import (
	"bufio"
	"bytes"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
)

// Type classifies a bounce.
type Type string

const (
	TypeHard      Type = "hard"      // permanent (5.x.x) - suppress
	TypeSoft      Type = "soft"      // transient (4.x.x)
	TypeComplaint Type = "complaint" // ARF feedback report
	TypeUnknown   Type = "unknown"
)

// Result is the parsed outcome of an inbound bounce/DSN message.
type Result struct {
	Type      Type
	Status    string // RFC 3463 status e.g. "5.1.1"
	DiagCode  string // Diagnostic-Code text
	Recipient string // Final-Recipient address if present
}

var (
	statusRe   = regexp.MustCompile(`\b([245])\.\d{1,3}\.\d{1,3}\b`)
	smtpCodeRe = regexp.MustCompile(`\b([245])\d\d\b`)
)

// Parse inspects a raw inbound message and classifies it. It handles
// multipart/report DSNs (RFC 3464), ARF complaints (message/feedback-report),
// and falls back to scanning the body for status codes / known phrases.
func Parse(raw []byte) Result {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return heuristic(raw)
	}
	ct := msg.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(ct)

	if strings.EqualFold(mediaType, "multipart/report") || strings.HasPrefix(mediaType, "multipart/") {
		if boundary := params["boundary"]; boundary != "" {
			if r, ok := parseMultipartReport(msg, boundary, params); ok {
				return r
			}
		}
	}
	// Not a structured report - scan the whole message.
	return heuristic(raw)
}

func parseMultipartReport(msg *mail.Message, boundary string, topParams map[string]string) (Result, bool) {
	reportType := strings.ToLower(topParams["report-type"])
	mr := multipart.NewReader(msg.Body, boundary)
	var res Result
	found := false
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		pType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		body := readAll(part)
		switch {
		case strings.EqualFold(pType, "message/feedback-report") || reportType == "feedback-report":
			res.Type = TypeComplaint
			found = true
		case strings.EqualFold(pType, "message/delivery-status"):
			res = mergeDeliveryStatus(res, body)
			found = true
		}
	}
	if !found {
		return res, false
	}
	if res.Type == "" {
		res.Type = classifyStatus(res.Status)
	}
	return res, true
}

// mergeDeliveryStatus parses the per-recipient DSN fields.
func mergeDeliveryStatus(res Result, body []byte) Result {
	sc := bufio.NewScanner(bytes.NewReader(body))
	var action string
	for sc.Scan() {
		line := sc.Text()
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "status:"):
			if m := statusRe.FindString(line); m != "" {
				res.Status = m
			}
		case strings.HasPrefix(lower, "action:"):
			action = strings.TrimSpace(line[len("action:"):])
		case strings.HasPrefix(lower, "diagnostic-code:"):
			res.DiagCode = strings.TrimSpace(line[len("diagnostic-code:"):])
		case strings.HasPrefix(lower, "final-recipient:"):
			v := strings.TrimSpace(line[len("final-recipient:"):])
			if i := strings.LastIndex(v, ";"); i >= 0 {
				v = strings.TrimSpace(v[i+1:])
			}
			res.Recipient = strings.Trim(v, "<>")
		}
	}
	res.Type = classifyStatus(res.Status)
	// "failed" action with no parseable 4/5 status → treat as hard.
	if res.Type == TypeUnknown && strings.EqualFold(action, "failed") {
		res.Type = TypeHard
	}
	return res
}

// classifyStatus maps an RFC 3463 status to a bounce type.
func classifyStatus(status string) Type {
	switch {
	case strings.HasPrefix(status, "5."):
		return TypeHard
	case strings.HasPrefix(status, "4."):
		return TypeSoft
	default:
		return TypeUnknown
	}
}

// heuristic scans non-conformant bounces for status codes / known phrases.
func heuristic(raw []byte) Result {
	text := string(raw)
	res := Result{Type: TypeUnknown}
	if m := statusRe.FindString(text); m != "" {
		res.Status = m
		res.Type = classifyStatus(m)
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "feedback-report") || strings.Contains(lower, "abuse report") {
		res.Type = TypeComplaint
		return res
	}
	if res.Type != TypeUnknown {
		return res
	}
	// Phrase heuristics for servers that send prose instead of a DSN.
	hard := []string{"user unknown", "no such user", "does not exist", "unknown recipient",
		"recipient rejected", "mailbox unavailable", "address rejected", "550 5.1.1", "recipient not found"}
	soft := []string{"mailbox full", "quota exceeded", "over quota", "try again later",
		"temporarily deferred", "greylist", "rate limited", "insufficient system storage"}
	for _, p := range hard {
		if strings.Contains(lower, p) {
			res.Type = TypeHard
			return res
		}
	}
	for _, p := range soft {
		if strings.Contains(lower, p) {
			res.Type = TypeSoft
			return res
		}
	}
	// Last resort: an SMTP-style 5xx/4xx code anywhere.
	if m := smtpCodeRe.FindString(text); m != "" {
		if strings.HasPrefix(m, "5") {
			res.Type = TypeHard
		} else if strings.HasPrefix(m, "4") {
			res.Type = TypeSoft
		}
	}
	return res
}

func readAll(r interface{ Read([]byte) (int, error) }) []byte {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.Bytes()
}
