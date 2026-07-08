package delivery

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

// fakeMX is an in-process destination SMTP server for tests.
type fakeMX struct {
	mu       sync.Mutex
	received [][]byte
	rcptErr  *smtp.SMTPError // returned from RCPT if set
}

func (f *fakeMX) NewSession(_ *smtp.Conn) (smtp.Session, error) { return &fakeSession{mx: f}, nil }

type fakeSession struct{ mx *fakeMX }

func (s *fakeSession) Mail(string, *smtp.MailOptions) error { return nil }
func (s *fakeSession) Rcpt(string, *smtp.RcptOptions) error {
	if s.mx.rcptErr != nil {
		return s.mx.rcptErr
	}
	return nil
}
func (s *fakeSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.mx.mu.Lock()
	s.mx.received = append(s.mx.received, b)
	s.mx.mu.Unlock()
	return nil
}
func (s *fakeSession) Reset()        {}
func (s *fakeSession) Logout() error { return nil }

func startFakeMX(t *testing.T, mx *fakeMX) (host, port string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := smtp.NewServer(mx)
	srv.Domain = "fake.mx"
	srv.AllowInsecureAuth = true
	go func() { _ = srv.Serve(ln) }()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p, func() { _ = srv.Close(); _ = ln.Close() }
}

func TestDeliverSuccess(t *testing.T) {
	mx := &fakeMX{}
	host, port, stop := startFakeMX(t, mx)
	defer stop()

	res := Deliver(context.Background(), DeliverInput{
		MXHosts: []string{host}, Port: port, EHLOName: "mail.test",
		MailFrom: "bounce-x@bounce.voxsub.example", Rcpt: "dest@example.net",
		Data: []byte("From: a@voxsub.example\r\nSubject: hi\r\n\r\nbody\r\n"), Timeout: 5 * time.Second,
	})
	if !res.Delivered {
		t.Fatalf("expected delivered, got %+v", res)
	}
	mx.mu.Lock()
	defer mx.mu.Unlock()
	if len(mx.received) != 1 || !strings.Contains(string(mx.received[0]), "body") {
		t.Fatalf("message not received by MX: %v", mx.received)
	}
}

func TestDeliverPermanentFailure(t *testing.T) {
	mx := &fakeMX{rcptErr: &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "no such user"}}
	host, port, stop := startFakeMX(t, mx)
	defer stop()

	res := Deliver(context.Background(), DeliverInput{
		MXHosts: []string{host}, Port: port, EHLOName: "mail.test",
		MailFrom: "b@x", Rcpt: "nobody@example.net", Data: []byte("x\r\n"), Timeout: 5 * time.Second,
	})
	if res.Delivered || !res.Permanent || res.Code != 550 {
		t.Fatalf("expected permanent 550, got %+v", res)
	}
}

func TestDeliverTransientFailure(t *testing.T) {
	mx := &fakeMX{rcptErr: &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "try later"}}
	host, port, stop := startFakeMX(t, mx)
	defer stop()

	res := Deliver(context.Background(), DeliverInput{
		MXHosts: []string{host}, Port: port, EHLOName: "mail.test",
		MailFrom: "b@x", Rcpt: "dest@example.net", Data: []byte("x\r\n"), Timeout: 5 * time.Second,
	})
	if res.Delivered || res.Permanent || res.Code != 451 {
		t.Fatalf("expected transient 451, got %+v", res)
	}
}

func TestDeliverConnectionRefusedIsTransient(t *testing.T) {
	res := Deliver(context.Background(), DeliverInput{
		MXHosts: []string{"127.0.0.1"}, Port: "1", EHLOName: "mail.test",
		MailFrom: "b@x", Rcpt: "d@e.net", Data: []byte("x"), Timeout: 2 * time.Second,
	})
	if res.Delivered || res.Permanent {
		t.Fatalf("connection failure must be transient, got %+v", res)
	}
}

func TestOrderedIPsFamilyPolicy(t *testing.T) {
	ctx := context.Background()
	// Literal IPv4 always allowed.
	if got := orderedIPs(ctx, net.DefaultResolver, "127.0.0.1", false); len(got) != 1 {
		t.Errorf("ipv4 literal: got %v", got)
	}
	// Literal IPv6 skipped when IPv6 disabled, included when enabled.
	if got := orderedIPs(ctx, net.DefaultResolver, "::1", false); len(got) != 0 {
		t.Errorf("ipv6 literal with useIPv6=false should be skipped, got %v", got)
	}
	if got := orderedIPs(ctx, net.DefaultResolver, "::1", true); len(got) != 1 {
		t.Errorf("ipv6 literal with useIPv6=true should be included, got %v", got)
	}
}

func TestLocalAddrBinding(t *testing.T) {
	in := DeliverInput{LocalIPv4: "160.30.37.130", LocalIPv6: "2001:df4:2040:5::2"}
	if la := localAddr(net.ParseIP("1.2.3.4"), in); la == nil || la.IP.String() != "160.30.37.130" {
		t.Errorf("ipv4 target should bind LocalIPv4, got %v", la)
	}
	if la := localAddr(net.ParseIP("2606:4700::1111"), in); la == nil || la.IP.String() != "2001:df4:2040:5::2" {
		t.Errorf("ipv6 target should bind LocalIPv6, got %v", la)
	}
	if la := localAddr(net.ParseIP("1.2.3.4"), DeliverInput{}); la != nil {
		t.Errorf("no configured source should bind nil, got %v", la)
	}
}
