package submission

import (
	"bytes"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// headerField is one raw header field (its first line plus any folded
// continuation lines), preserved verbatim for DKIM stability.
type headerField struct {
	name  string // canonical lower-case field name
	lines []string
}

// splitMessage splits raw into header block and body at the first blank line.
// It tolerates both CRLF and LF input.
func splitMessage(raw []byte) (header, body []byte) {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return raw[:i+2], raw[i+4:]
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return raw[:i+1], raw[i+2:]
	}
	return raw, nil
}

// parseFields parses a header block into ordered fields.
func parseFields(header []byte) []headerField {
	text := strings.ReplaceAll(string(header), "\r\n", "\n")
	var fields []headerField
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if len(fields) > 0 {
				fields[len(fields)-1].lines = append(fields[len(fields)-1].lines, line)
			}
			continue
		}
		name := line
		if c := strings.IndexByte(line, ':'); c >= 0 {
			name = line[:c]
		}
		fields = append(fields, headerField{name: strings.ToLower(strings.TrimSpace(name)), lines: []string{line}})
	}
	return fields
}

// assembleOptions controls header hygiene during assembly.
type assembleOptions struct {
	receivedHeader string    // our Received line value (without the "Received: " prefix)
	messageID      string    // full Message-ID value incl. angle brackets
	date           time.Time // used if Date missing
}

// assemble rewrites the message: strips inbound Received headers (they can leak
// app IPs), guarantees Message-ID and Date, prepends our Received, and emits
// CRLF line endings ready for signing and transmission.
func assemble(raw []byte, opt assembleOptions) []byte {
	header, body := splitMessage(raw)
	fields := parseFields(header)

	hasMessageID, hasDate := false, false
	var kept []headerField
	for _, f := range fields {
		switch f.name {
		case "received":
			continue // strip
		case "message-id":
			hasMessageID = true
		case "date":
			hasDate = true
		}
		kept = append(kept, f)
	}

	var out bytes.Buffer
	// Our Received first.
	out.WriteString("Received: " + opt.receivedHeader + "\r\n")
	if !hasMessageID && opt.messageID != "" {
		out.WriteString("Message-ID: " + opt.messageID + "\r\n")
	}
	if !hasDate {
		out.WriteString("Date: " + opt.date.UTC().Format(time.RFC1123Z) + "\r\n")
	}
	for _, f := range kept {
		for _, ln := range f.lines {
			out.WriteString(ln)
			out.WriteString("\r\n")
		}
	}
	out.WriteString("\r\n")
	// Normalise body to CRLF.
	out.Write(toCRLF(body))
	return out.Bytes()
}

// toCRLF converts bare LFs to CRLF without doubling existing CRLFs.
func toCRLF(b []byte) []byte {
	normalized := bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n"))
}

// headerFrom extracts the address in the From header (lower-cased).
func headerFrom(raw []byte) (string, error) {
	header, _ := splitMessage(raw)
	msg, err := mail.ReadMessage(bytes.NewReader(append(append([]byte{}, header...), '\r', '\n')))
	if err != nil {
		return "", fmt.Errorf("parse headers: %w", err)
	}
	from := msg.Header.Get("From")
	if from == "" {
		return "", fmt.Errorf("missing From header")
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return "", fmt.Errorf("invalid From header: %w", err)
	}
	return strings.ToLower(addr.Address), nil
}

// subjectOf returns the Subject header (may be empty).
func subjectOf(raw []byte) string {
	header, _ := splitMessage(raw)
	msg, err := mail.ReadMessage(bytes.NewReader(append(append([]byte{}, header...), '\r', '\n')))
	if err != nil {
		return ""
	}
	return msg.Header.Get("Subject")
}

// domainOf returns the domain part of an email address, lower-cased.
func domainOf(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}
