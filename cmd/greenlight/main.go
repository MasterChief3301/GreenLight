// Command greenlight runs the approval-broker web service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MasterChief3301/greenlight/internal/app"
	"github.com/MasterChief3301/greenlight/internal/config"
	"github.com/MasterChief3301/greenlight/internal/scheduler"
	"github.com/MasterChief3301/greenlight/internal/server"
	"github.com/MasterChief3301/greenlight/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Now that config is loaded, switch logging to the configured level.
	log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	a := app.New(st, cfg, log)

	// On a fresh install with no API keys, mint one so the operator can wire up
	// n8n immediately. Logged at Error so it surfaces even at the default log level.
	if n, err := st.CountAPIKeys(); err == nil && n == 0 {
		if key, err := app.BootstrapAPIKey(st); err == nil {
			log.Error("no API keys found — generated a bootstrap key (store it now; manage more under Settings)", "api_key", key)
		}
	}

	srv, err := server.New(a)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Root context cancelled on shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Background timeout/reminder engine.
	sched := scheduler.New(a, cfg.SchedulerInterval)
	go sched.Run(ctx)

	// Start HTTP server.
	serverErr := make(chan error, 1)
	go func() {
		// Logged at Error so the startup banner shows even at the default log level.
		log.Error("greenlight listening", "addr", cfg.Addr, "public_url", cfg.PublicURL,
			"ntfy", cfg.NtfyConfigured())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Graceful shutdown: stop new HTTP, let scheduler stop, wait for in-flight
	// callback deliveries.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown", "err", err)
	}
	a.Wait()
	log.Info("shutdown complete")
	return nil
}
