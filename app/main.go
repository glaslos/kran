package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/glaslos/kran/internal/config"
	"github.com/glaslos/kran/internal/docker"
	"github.com/glaslos/kran/internal/httpserver"
	"github.com/glaslos/kran/internal/metrics"
	"github.com/glaslos/kran/internal/notify"
	"github.com/glaslos/kran/internal/updater"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "kran: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	log := newLogger(cfg)

	dc, err := docker.New(cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer dc.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := dc.Ping(ctx); err != nil {
		return fmt.Errorf("docker ping: %w", err)
	}
	log.Info("connected to docker", "host", cfg.DockerHost)

	if cfg.NotifyURL != "" {
		if err := notify.Validate(cfg.NotifyURL); err != nil {
			return fmt.Errorf("notify-url: %w", err)
		}
	}

	if cfg.WebhookAPIKey != "" && cfg.HTTPAddr == "" {
		return fmt.Errorf("webhook-api-key requires -http-addr")
	}

	var onDemand <-chan struct{}
	var triggerTick chan struct{}
	if cfg.WebhookAPIKey != "" {
		triggerTick = make(chan struct{}, 1)
		onDemand = triggerTick
	}

	m := metrics.New()
	if cfg.HTTPAddr != "" {
		r := httpserver.NewRouter(log)
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Method(http.MethodGet, "/metrics", m.Handler())
		if cfg.WebhookAPIKey != "" {
			r.Post("/webhook/update", httpserver.WebhookUpdateHandler(cfg.WebhookAPIKey, func() {
				select {
				case triggerTick <- struct{}{}:
				default:
				}
			}))
		}

		go func() {
			if err := httpserver.Serve(ctx, log, cfg.HTTPAddr, r); err != nil {
				log.Error("http server stopped", "err", err)
				cancel()
			}
		}()
		log.Info("http listening", "addr", cfg.HTTPAddr)
	}

	return updater.Run(ctx, log, cfg, dc, m, onDemand)
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler
	if cfg.LogJSON {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
