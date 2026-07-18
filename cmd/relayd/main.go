// Command relayd is the single Relay server binary: API, WebUI, SMTP listeners,
// delivery workers, and background jobs share one process and one Postgres pool.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"relay/internal/api"
	"relay/internal/auth"
	"relay/internal/certs"
	"relay/internal/config"
	"relay/internal/crypto"
	"relay/internal/delivery"
	"relay/internal/dmarc"
	"relay/internal/dns"
	"relay/internal/retention"
	smtpin "relay/internal/smtp/inbound"
	smtpsub "relay/internal/smtp/submission"
	"relay/internal/stats"
	"relay/internal/storage"
	"relay/internal/store"
	"relay/internal/webhook"
	webui "relay/web"
)

// version is the build version reported by /v1/server/info.
const version = "0.9.0"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("starting relayd", "hostname", cfg.Hostname, "http_addr", cfg.HTTPAddr)
	// Surface the derived server identity so operators can verify it (especially
	// auto-detected sending IPs — pin them in relay.toml if the guess is wrong).
	logger.Info("server identity",
		"spf_include", cfg.SPFInclude, "dmarc_rua", cfg.DMARCRua,
		"sending_ipv4", cfg.SendingIPv4, "sending_ipv6", cfg.SendingIPv6)

	// Root context cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.AutoMigrate {
		logger.Info("applying migrations")
		if err := store.Migrate(cfg.DatabaseURL); err != nil {
			return err
		}
	}

	st, err := store.Connect(ctx, cfg.DatabaseURL, cfg.MaxConns)
	if err != nil {
		return err
	}
	defer st.Close()
	logger.Info("database connected")

	dist, err := webui.Dist()
	if err != nil {
		logger.Warn("embedded SPA unavailable", "err", err)
	}

	sealer, err := crypto.NewSealer(cfg.SecretKeyB64)
	if err != nil {
		return fmt.Errorf("secret key: %w", err)
	}
	dnsParams := dns.Params{Hostname: cfg.Hostname, SPFInclude: cfg.SPFInclude, DMARCRua: cfg.DMARCRua}
	verifier := dns.NewVerifier(dnsParams, cfg.DNSResolvers)

	if err := api.SeedAdminUser(ctx, st, cfg.AdminUser, cfg.AdminPassword); err != nil {
		logger.Warn("seed admin user", "err", err)
	}

	blobs, err := storage.New(cfg.StorageDir)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	srv := &api.Server{
		Store:                   st,
		Log:                     logger,
		Dist:                    dist,
		Tokens:                  cfg.AdminTokens,
		Sealer:                  sealer,
		Verifier:                verifier,
		Params:                  dnsParams,
		SendingIPv4:             cfg.SendingIPv4,
		SendingIPv6:             cfg.SendingIPv6,
		Blobs:                   blobs,
		Hostname:                cfg.Hostname,
		Version:                 version,
		TLSEnabled:              cfg.TLSEnabled,
		MetricsAddr:             cfg.MetricsAddr,
		RetentionDefaultEnabled: cfg.RetentionEnabled,
		RetentionDefaultDays:    int(cfg.RetentionMetadata.Hours() / 24),
		ListenerAddrs: map[string]string{
			"http": cfg.HTTPAddr, "submission": cfg.SubmissionAddr,
			"submissions": cfg.SubmissionsTLS, "inbound": cfg.InboundAddr,
		},
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// When a dedicated metrics address is configured, serve Prometheus there on
	// its own listener (IP-restricted by ops/firewall) instead of the public mux.
	var metricsServer *http.Server
	if cfg.MetricsAddr != "" {
		mmux := http.NewServeMux()
		mmux.Handle("/metrics", api.MetricsHandler())
		metricsServer = &http.Server{Addr: cfg.MetricsAddr, Handler: mmux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			logger.Info("metrics listening", "addr", cfg.MetricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server", "err", err)
			}
		}()
	}

	// Background DNS re-verifier.
	reverifier := &dns.Reverifier{Store: st, Verifier: verifier, Log: logger, Interval: cfg.DNSReverifyInterval}
	go reverifier.Run(ctx)

	// Hourly stats rollups for dashboard time-series.
	go (&stats.Roller{Store: st, Log: logger}).Run(ctx)

	// Retention sweeps (per-direction body + metadata cleanup).
	if cfg.RetentionEnabled {
		go (&retention.Worker{
			Store: st, Blobs: blobs, Log: logger,
			Interval:       cfg.RetentionInterval,
			OutboundBodies: cfg.RetentionOutboundBodies,
			InboundBodies:  cfg.RetentionInboundBodies,
			Metadata:       cfg.RetentionMetadata,
		}).Run(ctx)
		logger.Info("retention worker started", "interval", cfg.RetentionInterval)
	}

	// Outbound delivery workers.
	if cfg.DeliveryEnabled {
		pool := &delivery.Pool{
			Store: st, Blobs: blobs, Log: logger, Hostname: cfg.Hostname,
			WorkerID:    cfg.Hostname,
			Concurrency: cfg.DeliveryConcurrency, PerDomain: cfg.DeliveryPerDomain,
			LocalIPv4: cfg.SendingIPv4, LocalIPv6: cfg.SendingIPv6, UseIPv6: cfg.DeliverIPv6, Sink: cfg.SMTPSink,
			Retry: delivery.RetryPolicy{Schedule: cfg.DeliveryRetrySchedule, MaxAge: cfg.DeliveryMaxAge},
			Metrics: delivery.Metrics{
				Delivered:     promauto.NewCounter(prometheus.CounterOpts{Name: "relay_delivered_total", Help: "Recipients delivered."}),
				Deferred:      promauto.NewCounter(prometheus.CounterOpts{Name: "relay_deferred_total", Help: "Delivery attempts deferred."}),
				Failed:        promauto.NewCounter(prometheus.CounterOpts{Name: "relay_failed_total", Help: "Recipients permanently failed."}),
				QueueDepth:    promauto.NewGauge(prometheus.GaugeOpts{Name: "relay_queue_depth", Help: "Delivery jobs awaiting send."}),
				Latency:       promauto.NewHistogram(prometheus.HistogramOpts{Name: "relay_delivery_seconds", Help: "Per-attempt delivery latency.", Buckets: prometheus.DefBuckets}),
				DeferByDomain: promauto.NewCounterVec(prometheus.CounterOpts{Name: "relay_deferrals_by_domain_total", Help: "Deferrals per destination domain."}, []string{"domain"}),
			},
		}
		go pool.Run(ctx)
		logger.Info("delivery workers started", "concurrency", cfg.DeliveryConcurrency)
	}

	// TLS: certmagic-managed Let's Encrypt cert (shared by HTTPS + all SMTP
	// listeners) when enabled, else a self-signed cert for dev.
	var smtpTLS *tls.Config
	var challengeSrv *http.Server
	httpsMode := false
	if cfg.TLSEnabled {
		mgr, err := certs.NewManager(ctx, certs.ACMEConfig{
			Hostname: cfg.Hostname, Email: cfg.ACMEEmail,
			StorageDir: filepath.Join(cfg.StorageDir, "acme"),
			Staging:    cfg.ACMEStaging, CA: cfg.ACMECA,
		})
		if err != nil {
			return err
		}
		// ACME HTTP-01 challenge + HTTPS redirect on :80, up before issuance.
		challengeSrv = &http.Server{
			Addr:              cfg.ACMEHTTPAddr,
			Handler:           mgr.ChallengeHandler(redirectToHTTPS(cfg.Hostname)),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if err := challengeSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("acme/redirect (:80) listener", "err", err)
			}
		}()
		logger.Info("obtaining certificate", "hostname", cfg.Hostname, "staging", cfg.ACMEStaging)
		if err := mgr.Obtain(ctx, cfg.Hostname); err != nil {
			return fmt.Errorf("obtain certificate: %w", err)
		}
		httpServer.TLSConfig = mgr.HTTPSTLSConfig()
		smtpTLS = mgr.SMTPTLSConfig()
		srv.CertExpiry = func() (time.Time, bool) { return mgr.LeafNotAfter(cfg.Hostname) }
		httpsMode = true
		logger.Info("TLS enabled (ACME)", "hostname", cfg.Hostname)
	} else if cfg.SubmissionEnabled || cfg.InboundEnabled {
		cert, err := certs.SelfSigned(cfg.Hostname)
		if err != nil {
			return fmt.Errorf("self-signed cert: %w", err)
		}
		smtpTLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	// Webhook dispatcher (delivers inbound-mail webhooks with retries).
	dispatcher := &webhook.Dispatcher{
		Store: st, Blobs: blobs, Sealer: sealer, Log: logger,
		Schedule: cfg.WebhookRetrySchedule, MaxAge: cfg.WebhookMaxAge,
		Metrics: webhook.Metrics{
			Delivered:  promauto.NewCounter(prometheus.CounterOpts{Name: "relay_webhooks_delivered_total", Help: "Webhooks delivered."}),
			DeadLetter: promauto.NewCounter(prometheus.CounterOpts{Name: "relay_webhooks_dead_letter_total", Help: "Webhooks dead-lettered."}),
		},
	}
	go dispatcher.Run(ctx)

	// Submission backend (used by 587/465 and, for trusted subnets, by :25).
	submitted := promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_messages_submitted_total",
		Help: "Messages accepted via submission listeners.",
	})
	subBackend := smtpsub.New(smtpsub.Deps{
		Store: st, Auth: auth.NewAuthenticator(st, auth.DefaultConfig()),
		Sealer: sealer, Blobs: blobs, Log: logger, Hostname: cfg.Hostname,
		MaxMessageBytes: cfg.MaxMessageBytes, Submitted: submitted,
	})

	// SMTP submission listeners (587 STARTTLS, 465 implicit TLS).
	var smtpServers []*smtp.Server
	if cfg.SubmissionEnabled {
		backend := subBackend
		srv587 := smtpsub.NewServer(cfg.SubmissionAddr, cfg.Hostname, backend, smtpTLS, cfg.MaxMessageBytes)
		srv465 := smtpsub.NewServer(cfg.SubmissionsTLS, cfg.Hostname, backend, smtpTLS, cfg.MaxMessageBytes)
		smtpServers = append(smtpServers, srv587, srv465)

		go func() {
			logger.Info("smtp submission listening (STARTTLS)", "addr", cfg.SubmissionAddr)
			if err := srv587.ListenAndServe(); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
				logger.Error("587 listener", "err", err)
			}
		}()
		go func() {
			logger.Info("smtp submission listening (implicit TLS)", "addr", cfg.SubmissionsTLS)
			if err := srv465.ListenAndServeTLS(); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
				logger.Error("465 listener", "err", err)
			}
		}()
	}

	// Inbound listener (port 25): async bounces now, mailboxes in Phase 6.
	if cfg.InboundEnabled {
		inb := smtpin.New(smtpin.Deps{
			Store: st, Blobs: blobs, Log: logger, Hostname: cfg.Hostname, MaxMessageBytes: cfg.MaxMessageBytes,
			Submission: subBackend, AuthSubnets: parseCIDRs(cfg.Port25AuthSubnets, logger),
			DMARC: &dmarc.Ingester{Store: st, Blobs: blobs, Log: logger},
		})
		srv25 := smtpin.NewServer(cfg.InboundAddr, cfg.Hostname, inb, smtpTLS, cfg.MaxMessageBytes)
		smtpServers = append(smtpServers, srv25)
		ln25, lerr := net.Listen("tcp", cfg.InboundAddr)
		if lerr != nil {
			return fmt.Errorf("listen inbound %s: %w", cfg.InboundAddr, lerr)
		}
		ln25 = smtpin.LimitListener(ln25, cfg.InboundMaxConns, cfg.InboundMaxConnsPerIP, logger)
		go func() {
			logger.Info("smtp inbound listening", "addr", cfg.InboundAddr,
				"max_conns", cfg.InboundMaxConns, "max_conns_per_ip", cfg.InboundMaxConnsPerIP)
			if err := srv25.Serve(ln25); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
				logger.Error("25 listener", "err", err)
			}
		}()
	}

	// Serve HTTP(S).
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr, "tls", httpsMode)
		var err error
		if httpsMode {
			err = httpServer.ListenAndServeTLS("", "") // cert from TLSConfig
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown: stop accepting SMTP, then drain HTTP.
	for _, srv := range smtpServers {
		if err := srv.Close(); err != nil {
			logger.Warn("smtp close", "err", err)
		}
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if challengeSrv != nil {
		_ = challengeSrv.Shutdown(shutCtx)
	}
	if metricsServer != nil {
		_ = metricsServer.Shutdown(shutCtx)
	}
	if err := httpServer.Shutdown(shutCtx); err != nil {
		logger.Warn("http shutdown", "err", err)
	}
	logger.Info("stopped")
	return nil
}

// parseCIDRs parses CIDR strings, logging and skipping invalid ones.
func parseCIDRs(cidrs []string, logger *slog.Logger) []*net.IPNet {
	var out []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			logger.Warn("invalid port25 auth subnet", "cidr", c, "err", err)
			continue
		}
		out = append(out, n)
	}
	return out
}

// redirectToHTTPS 301-redirects plain HTTP to HTTPS on the server hostname
// (non-ACME requests on :80).
func redirectToHTTPS(hostname string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "https://" + hostname + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
