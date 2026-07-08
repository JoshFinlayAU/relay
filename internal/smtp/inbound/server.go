package inbound

import (
	"crypto/tls"
	"time"

	"github.com/emersion/go-smtp"
)

// NewServer builds the port-25 inbound server. STARTTLS is offered
// opportunistically; no AUTH (this listener never relays).
func NewServer(addr, hostname string, b *Backend, tlsConf *tls.Config, maxBytes int64) *smtp.Server {
	s := smtp.NewServer(b)
	s.Addr = addr
	s.Domain = hostname
	s.TLSConfig = tlsConf
	s.AllowInsecureAuth = false
	s.MaxMessageBytes = maxBytes
	s.MaxRecipients = 100
	s.ReadTimeout = 5 * time.Minute
	s.WriteTimeout = 5 * time.Minute
	return s
}
