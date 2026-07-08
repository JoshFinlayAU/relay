package submission

import (
	"crypto/tls"
	"time"

	"github.com/emersion/go-smtp"
)

// NewServer builds a configured go-smtp server for the submission backend.
// AllowInsecureAuth is false so AUTH is only offered after STARTTLS (587) or on
// an implicitly-TLS connection (465).
func NewServer(addr, hostname string, b *Backend, tlsConf *tls.Config, maxBytes int64) *smtp.Server {
	s := smtp.NewServer(b)
	s.Addr = addr
	s.Domain = hostname
	s.TLSConfig = tlsConf
	s.AllowInsecureAuth = false
	s.MaxMessageBytes = maxBytes
	s.MaxRecipients = 100
	s.ReadTimeout = 2 * time.Minute
	s.WriteTimeout = 2 * time.Minute
	s.MaxLineLength = 2000
	return s
}
