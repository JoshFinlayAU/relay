// Package webhook parses inbound MIME into a JSON payload and dispatches it to
// a mailbox's webhook URL with an HMAC signature, retries, and dead-lettering.
package webhook

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"

	"github.com/emersion/go-message/mail"

	"relay/internal/storage"
)

// InlineAttachmentLimit is the max attachment size embedded as base64 inline;
// larger attachments are stored and referenced.
const InlineAttachmentLimit = 256 * 1024

// Attachment is one MIME attachment in the webhook payload.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Content     string `json:"content,omitempty"` // base64, when inline
	Ref         string `json:"ref,omitempty"`     // storage ref, when too large to inline
}

// Payload is the JSON body POSTed to a mailbox webhook (CLAUDE.md schema).
type Payload struct {
	MessageID   string            `json:"message_id"`
	From        string            `json:"from"`
	To          []string          `json:"to"`
	Subject     string            `json:"subject"`
	Headers     map[string]string `json:"headers"`
	Text        string            `json:"text"`
	HTML        string            `json:"html"`
	Attachments []Attachment      `json:"attachments"`
	RawSize     int               `json:"raw_size"`
	SPFResult   string            `json:"spf_result"`
	DKIMResult  string            `json:"dkim_result"`
}

// BuildPayload parses a raw inbound message into a Payload. Large attachments
// are written to blobs and referenced. Parsing errors degrade gracefully (the
// payload still carries envelope metadata and whatever parsed).
func BuildPayload(raw []byte, blobs *storage.Store) Payload {
	p := Payload{RawSize: len(raw), Headers: map[string]string{}, Attachments: []Attachment{}, To: []string{}}

	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return p
	}
	// Selected headers.
	h := mr.Header
	p.Subject, _ = h.Subject()
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		p.From = from[0].Address
	}
	if to, err := h.AddressList("To"); err == nil {
		for _, a := range to {
			p.To = append(p.To, a.Address)
		}
	}
	for _, key := range []string{"Message-Id", "Date", "Reply-To", "Subject", "From", "To", "Cc"} {
		if v := h.Get(key); v != "" {
			p.Headers[key] = v
		}
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch hdr := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := hdr.ContentType()
			body, _ := io.ReadAll(part.Body)
			switch {
			case strings.EqualFold(ct, "text/html"):
				if p.HTML == "" {
					p.HTML = string(body)
				}
			case strings.HasPrefix(ct, "text/"):
				if p.Text == "" {
					p.Text = string(body)
				}
			}
		case *mail.AttachmentHeader:
			ct, _, _ := hdr.ContentType()
			filename, _ := hdr.Filename()
			body, _ := io.ReadAll(part.Body)
			att := Attachment{Filename: filename, ContentType: ct, Size: len(body)}
			if len(body) <= InlineAttachmentLimit {
				att.Content = base64.StdEncoding.EncodeToString(body)
			} else if blobs != nil {
				if ref, err := blobs.Put(body); err == nil {
					att.Ref = ref
				}
			}
			p.Attachments = append(p.Attachments, att)
		}
	}
	return p
}
