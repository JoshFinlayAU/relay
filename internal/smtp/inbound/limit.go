package inbound

import (
	"log/slog"
	"net"
	"sync"
)

// limitListener wraps a net.Listener to cap total concurrent connections and
// concurrent connections per remote IP. Excess connections are accepted and
// immediately closed (SMTP peers will retry), so the accept loop never blocks
// and slow-loris/flood attempts from a single source can't exhaust the server.
type limitListener struct {
	net.Listener
	log      *slog.Logger
	maxTotal int
	maxPerIP int

	mu    sync.Mutex
	total int
	perIP map[string]int
}

// LimitListener returns ln wrapped with total/per-IP connection caps. A cap <= 0
// disables that dimension. When both are disabled the original listener is
// returned unchanged.
func LimitListener(ln net.Listener, maxTotal, maxPerIP int, log *slog.Logger) net.Listener {
	if maxTotal <= 0 && maxPerIP <= 0 {
		return ln
	}
	return &limitListener{Listener: ln, log: log, maxTotal: maxTotal, maxPerIP: maxPerIP, perIP: map[string]int{}}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ip := remoteIP(c.RemoteAddr())
		if l.reserve(ip) {
			return &limitConn{Conn: c, l: l, ip: ip}, nil
		}
		if l.log != nil {
			l.log.Warn("inbound connection rejected (limit)", "ip", ip)
		}
		_ = c.Close()
	}
}

// reserve records a new connection if within limits; returns false if over.
func (l *limitListener) reserve(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxTotal > 0 && l.total >= l.maxTotal {
		return false
	}
	if l.maxPerIP > 0 && l.perIP[ip] >= l.maxPerIP {
		return false
	}
	l.total++
	l.perIP[ip]++
	return true
}

func (l *limitListener) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total > 0 {
		l.total--
	}
	if l.perIP[ip] > 0 {
		if l.perIP[ip]--; l.perIP[ip] == 0 {
			delete(l.perIP, ip)
		}
	}
}

func remoteIP(a net.Addr) string {
	if host, _, err := net.SplitHostPort(a.String()); err == nil {
		return host
	}
	return a.String()
}

// limitConn decrements the counters exactly once when closed.
type limitConn struct {
	net.Conn
	l      *limitListener
	ip     string
	closed sync.Once
}

func (c *limitConn) Close() error {
	c.closed.Do(func() { c.l.release(c.ip) })
	return c.Conn.Close()
}
