// Package webhook parses inbound MIME into a JSON payload and dispatches it to
// a mailbox's webhook URL with an HMAC signature, retries, and dead-lettering.
package webhook

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-message/mail"

	"relay/internal/crypto"
	"relay/internal/storage"
)

// AttachmentURLTTLSeconds is how long a signed attachment URL stays valid.
const AttachmentURLTTLSeconds = 7 * 24 * 3600

// attachmentSignMsg is the canonical string signed for an attachment URL.
func attachmentSignMsg(msgID string, index int, exp int64) string {
	return fmt.Sprintf("att:%s:%d:%d", msgID, index, exp)
}

// SignedAttachmentURL builds a self-authenticating download URL for one
// attachment: <base>/v1/messages/{id}/attachments/{index}?exp=&sig=. The
// signature (HMAC via the sealer) lets a webhook receiver fetch it without an
// API token.
func SignedAttachmentURL(sealer *crypto.Sealer, baseURL, msgID string, index int, exp int64) string {
	sig := sealer.SignHex(attachmentSignMsg(msgID, index, exp))
	return fmt.Sprintf("%s/v1/messages/%s/attachments/%d?exp=%d&sig=%s",
		strings.TrimRight(baseURL, "/"), msgID, index, exp, sig)
}

// VerifyAttachmentSig validates a signed attachment URL's exp+sig.
func VerifyAttachmentSig(sealer *crypto.Sealer, msgID string, index int, exp, now int64, sig string) bool {
	if exp < now {
		return false
	}
	return sealer.VerifyHex(attachmentSignMsg(msgID, index, exp), sig)
}

// InlineAttachmentLimit is the max attachment size embedded as base64 inline;
// larger attachments are stored and referenced.
const InlineAttachmentLimit = 256 * 1024

// Attachment is one MIME attachment in the webhook payload.
type Attachment struct {
	Index       int    `json:"index"` // stable position in the message
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Content     string `json:"content,omitempty"` // base64, when inline (small)
	Ref         string `json:"ref,omitempty"`     // storage ref, when too large to inline
	RefURL      string `json:"ref_url,omitempty"` // signed download URL for a ref'd attachment
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
			att := Attachment{Index: len(p.Attachments), Filename: filename, ContentType: ct, Size: len(body)}
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

// ExtractedAttachment is a single attachment's decoded bytes + metadata.
type ExtractedAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ExtractAttachment reparses raw and returns the attachment at index (0-based),
// matching the ordering BuildPayload uses. ok is false if there is no such
// attachment or the message can't be parsed.
func ExtractAttachment(raw []byte, index int) (*ExtractedAttachment, bool) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	n := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		hdr, ok := part.Header.(*mail.AttachmentHeader)
		if !ok {
			continue
		}
		if n == index {
			ct, _, _ := hdr.ContentType()
			filename, _ := hdr.Filename()
			data, _ := io.ReadAll(part.Body)
			return &ExtractedAttachment{Filename: filename, ContentType: ct, Data: data}, true
		}
		n++
	}
	return nil, false
}
