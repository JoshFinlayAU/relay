// Package bounce handles VERP return-path encoding/decoding so async bounces
// route back to us and map to the exact message. DSN parsing lands in Phase 5.
package bounce

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// verpPrefix is the local-part prefix for VERP return-path addresses.
const verpPrefix = "bounce-"

// VERPAddress builds the MAIL FROM return path for a message:
//
//	bounce-<msgid>@bounce.<domain>
//
// bounceSubdomain is the domain's bounce subdomain (e.g. "bounce.example.com").
func VERPAddress(msgID uuid.UUID, bounceSubdomain string) string {
	return fmt.Sprintf("%s%s@%s", verpPrefix, msgID.String(), bounceSubdomain)
}

// DecodeVERP extracts the message ID from a VERP recipient address. It returns
// ok=false for anything that isn't a VERP bounce address.
func DecodeVERP(addr string) (uuid.UUID, bool) {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return uuid.Nil, false
	}
	local := addr[:at]
	if !strings.HasPrefix(local, verpPrefix) {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimPrefix(local, verpPrefix))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
