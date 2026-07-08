package inbound

import (
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeConn/fakeAddr are minimal net.Conn/net.Addr for the listener under test.
type fakeAddr struct{ s string }

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return a.s }

type fakeConn struct {
	addr   fakeAddr
	closed bool
	mu     *sync.Mutex
}

func (c *fakeConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *fakeConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *fakeConn) Close() error                     { c.mu.Lock(); c.closed = true; c.mu.Unlock(); return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return c.addr }
func (c *fakeConn) RemoteAddr() net.Addr             { return c.addr }
func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }

// scriptListener yields a fixed sequence of connections then blocks (returns EOF).
type scriptListener struct {
	conns []net.Conn
	i     int
}

func (l *scriptListener) Accept() (net.Conn, error) {
	if l.i >= len(l.conns) {
		return nil, io.EOF
	}
	c := l.conns[l.i]
	l.i++
	return c, nil
}
func (l *scriptListener) Close() error   { return nil }
func (l *scriptListener) Addr() net.Addr { return fakeAddr{"0.0.0.0:25"} }

func TestLimitListenerPerIP(t *testing.T) {
	mu := &sync.Mutex{}
	// Three conns from the same IP; per-IP cap of 2 ⇒ the third is closed by Accept.
	c1 := &fakeConn{addr: fakeAddr{"9.9.9.9:1001"}, mu: mu}
	c2 := &fakeConn{addr: fakeAddr{"9.9.9.9:1002"}, mu: mu}
	c3 := &fakeConn{addr: fakeAddr{"9.9.9.9:1003"}, mu: mu}
	inner := &scriptListener{conns: []net.Conn{c1, c2, c3}}
	ll := LimitListener(inner, 0, 2, slog.New(slog.NewTextHandler(io.Discard, nil)))

	a, err := ll.Accept()
	if err != nil {
		t.Fatalf("accept1: %v", err)
	}
	b, err := ll.Accept()
	if err != nil {
		t.Fatalf("accept2: %v", err)
	}
	// The third exceeds per-IP cap; Accept closes it internally and then hits EOF.
	if _, err := ll.Accept(); err != io.EOF {
		t.Fatalf("accept3 err = %v, want EOF (c3 rejected then listener drained)", err)
	}
	mu.Lock()
	if !c3.closed {
		t.Error("over-limit connection was not closed")
	}
	mu.Unlock()

	// Closing an accepted conn frees a per-IP slot.
	_ = a.Close()
	inner2 := &scriptListener{conns: []net.Conn{&fakeConn{addr: fakeAddr{"9.9.9.9:1004"}, mu: mu}}}
	llc := ll.(*limitListener)
	llc.Listener = inner2
	inner2.i = 0
	if _, err := ll.Accept(); err != nil {
		t.Errorf("accept after release: %v", err)
	}
	_ = b.Close()
}

func TestLimitListenerDisabled(t *testing.T) {
	inner := &scriptListener{}
	if got := LimitListener(inner, 0, 0, nil); got != inner {
		t.Error("both caps disabled should return the original listener")
	}
}
