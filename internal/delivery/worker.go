package delivery

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"

	"relay/internal/storage"
	"relay/internal/store"
)

func i32(v int) *int32 {
	if v == 0 {
		return nil
	}
	n := int32(v)
	return &n
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func boolp(b bool) *bool { return &b }

// Metrics are optional Prometheus instruments (nil-safe).
type Metrics struct {
	Delivered     prometheus.Counter
	Deferred      prometheus.Counter
	Failed        prometheus.Counter
	QueueDepth    prometheus.Gauge
	Latency       prometheus.Histogram   // per-attempt seconds
	DeferByDomain *prometheus.CounterVec // labelled by destination domain
}

// Pool runs the outbound delivery workers.
type Pool struct {
	Store       *store.Store
	Blobs       *storage.Store
	Log         *slog.Logger
	Hostname    string
	WorkerID    string
	Concurrency int           // number of worker goroutines
	PerDomain   int           // max concurrent connections per destination domain
	Timeout     time.Duration // per-attempt timeout
	Resolver    *net.Resolver
	LocalIPv4   string      // source address bound for IPv4 delivery (PTR/SPF-correct)
	LocalIPv6   string      // source address bound for IPv6 delivery
	UseIPv6     bool        // send over IPv6 (off until IPv6 PTR/AAAA are sound)
	Sink        string      // host:port - if set, ALL mail goes here (load testing), no MX lookup
	Retry       RetryPolicy // 4xx backoff schedule + give-up age (config-driven)
	Metrics     Metrics

	poll     time.Duration
	domainMu sync.Mutex
	domSems  map[string]chan struct{}
}

// Run starts the workers and the stale-job reaper; it blocks until ctx is done,
// then drains in-flight deliveries.
func (p *Pool) Run(ctx context.Context) {
	if p.Concurrency <= 0 {
		p.Concurrency = 8
	}
	if p.PerDomain <= 0 {
		p.PerDomain = 2
	}
	if len(p.Retry.Schedule) == 0 || p.Retry.MaxAge <= 0 {
		p.Retry = DefaultRetryPolicy()
	}
	if p.poll <= 0 {
		p.poll = time.Second
	}
	p.domSems = make(map[string]chan struct{})

	go p.reaper(ctx)

	var wg sync.WaitGroup
	for i := 0; i < p.Concurrency; i++ {
		wg.Add(1)
		go p.worker(ctx, &wg)
	}
	<-ctx.Done()
	wg.Wait()
}

func (p *Pool) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	id := p.WorkerID
	for {
		if ctx.Err() != nil {
			return
		}
		jobs, err := p.Store.ClaimDeliveryJobs(ctx, store.ClaimDeliveryJobsParams{LockedBy: &id, Limit: 1})
		if err != nil || len(jobs) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.poll):
			}
			continue
		}
		// Deliver with a fresh (non-cancelled) context so an in-flight send can
		// finish during graceful shutdown.
		p.process(context.Background(), jobs[0])
	}
}

func (p *Pool) process(ctx context.Context, job store.DeliveryJob) {
	msg, err := p.Store.GetMessage(ctx, job.MessageID)
	if err != nil {
		p.Log.Error("delivery: get message", "job", job.ID, "err", err)
		p.deferOrFail(ctx, job, 0, "internal: message missing")
		return
	}
	if msg.BodyRef == nil {
		p.failJob(ctx, job, 0, "internal: no body")
		return
	}
	data, err := p.Blobs.Get(*msg.BodyRef)
	if err != nil {
		p.deferOrFail(ctx, job, 0, "internal: body unreadable")
		return
	}
	mailFrom := ""
	if msg.MailFrom != nil {
		mailFrom = *msg.MailFrom
	}

	domain := domainOf(job.Rcpt)
	release := p.acquireDomain(domain)
	defer release()

	in := DeliverInput{
		EHLOName: p.Hostname, MailFrom: mailFrom, Rcpt: job.Rcpt, Data: data, Timeout: p.Timeout,
		Resolver: p.Resolver, LocalIPv4: p.LocalIPv4, LocalIPv6: p.LocalIPv6, UseIPv6: p.UseIPv6,
	}
	if p.Sink != "" {
		// Load-test sink: deliver everything to a fixed host:port, no MX lookup.
		host, port, _ := net.SplitHostPort(p.Sink)
		in.MXHosts = []string{host}
		in.Port = port
	} else {
		hosts, err := ResolveMX(ctx, p.Resolver, domain)
		if err != nil || len(hosts) == 0 {
			p.deferOrFail(ctx, job, 0, "no MX for "+domain)
			return
		}
		in.MXHosts = hosts
	}

	start := time.Now()
	res := Deliver(ctx, in)
	if p.Metrics.Latency != nil {
		p.Metrics.Latency.Observe(time.Since(start).Seconds())
	}

	p.record(ctx, job, res)

	switch {
	case res.Delivered:
		_ = p.Store.MarkJobDelivered(ctx, store.MarkJobDeliveredParams{ID: job.ID, LastCode: i32(res.Code), LastResponse: strp(res.Response)})
		if p.Metrics.Delivered != nil {
			p.Metrics.Delivered.Inc()
		}
		p.Log.Info("delivered", "msg", job.MessageID, "rcpt", job.Rcpt, "mx", res.MXHost, "tls", res.TLSVersion)
	case res.Permanent:
		p.failJob(ctx, job, res.Code, res.Response)
	default:
		if p.Metrics.DeferByDomain != nil {
			p.Metrics.DeferByDomain.WithLabelValues(domain).Inc()
		}
		p.deferOrFail(ctx, job, res.Code, res.Response)
	}
	p.updateMessageStatus(ctx, job.MessageID)
}

// acquireDomain enforces the per-domain concurrency cap.
func (p *Pool) acquireDomain(domain string) func() {
	p.domainMu.Lock()
	sem, ok := p.domSems[domain]
	if !ok {
		sem = make(chan struct{}, p.PerDomain)
		p.domSems[domain] = sem
	}
	p.domainMu.Unlock()
	sem <- struct{}{}
	return func() { <-sem }
}

func (p *Pool) record(ctx context.Context, job store.DeliveryJob, res Result) {
	result := "deferred"
	if res.Delivered {
		result = "delivered"
	} else if res.Permanent {
		result = "failed"
	}
	_ = p.Store.InsertDeliveryAttempt(ctx, store.InsertDeliveryAttemptParams{
		MessageID:    job.MessageID,
		Rcpt:         job.Rcpt,
		MxHost:       strp(res.MXHost),
		Result:       result,
		SmtpCode:     i32(res.Code),
		SmtpResponse: strp(res.Response),
		TlsVersion:   strp(res.TLSVersion),
		TlsVerified:  boolp(res.TLSVerified),
	})
}

func (p *Pool) deferOrFail(ctx context.Context, job store.DeliveryJob, code int, resp string) {
	if job.CreatedAt.Valid && p.Retry.GiveUp(job.CreatedAt.Time, time.Now()) {
		p.failJob(ctx, job, code, "gave up after max age: "+resp)
		return
	}
	next := pgtype.Timestamptz{Time: time.Now().Add(p.Retry.NextDelay(int(job.Attempts))), Valid: true}
	_ = p.Store.DeferJob(ctx, store.DeferJobParams{ID: job.ID, NextAttemptAt: next, LastCode: i32(code), LastResponse: strp(resp)})
	if p.Metrics.Deferred != nil {
		p.Metrics.Deferred.Inc()
	}
	p.Log.Info("deferred", "msg", job.MessageID, "rcpt", job.Rcpt, "attempts", job.Attempts, "resp", resp)
}

func (p *Pool) failJob(ctx context.Context, job store.DeliveryJob, code int, resp string) {
	_ = p.Store.FailJob(ctx, store.FailJobParams{ID: job.ID, LastCode: i32(code), LastResponse: strp(resp)})
	if p.Metrics.Failed != nil {
		p.Metrics.Failed.Inc()
	}
	p.Log.Info("failed", "msg", job.MessageID, "rcpt", job.Rcpt, "code", code, "resp", resp)
}

// updateMessageStatus rolls the per-recipient job states up to the message.
func (p *Pool) updateMessageStatus(ctx context.Context, id uuid.UUID) {
	counts, err := p.Store.JobStatusCounts(ctx, id)
	if err != nil {
		return
	}
	var status string
	switch {
	case counts.Pending > 0:
		return // still in flight; leave as queued
	case counts.Failed > 0 && counts.Delivered > 0:
		status = "partial"
	case counts.Failed > 0:
		status = "failed"
	default:
		status = "delivered"
	}
	_ = p.Store.SetMessageStatus(ctx, store.SetMessageStatusParams{ID: id, Status: status})
}

// reaper recovers jobs whose worker died mid-flight and refreshes the
// queue-depth gauge.
func (p *Pool) reaper(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	stale := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if p.Metrics.QueueDepth != nil {
				if depth, err := p.Store.QueueDepth(ctx); err == nil {
					p.Metrics.QueueDepth.Set(float64(depth))
				}
			}
			// Requeue stale in-progress jobs every ~2 minutes.
			if stale++; stale >= 4 {
				stale = 0
				cutoff := pgtype.Timestamptz{Time: time.Now().Add(-10 * time.Minute), Valid: true}
				_ = p.Store.RequeueStaleJobs(ctx, cutoff)
			}
		}
	}
}

func domainOf(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}
