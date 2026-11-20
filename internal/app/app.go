// Package app wires configuration, storage, the board service and the SSH
// server, and owns process lifecycle.
package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"charm.land/ssh"

	"github.com/mynameis-nigel/ssh-bulletin/internal/board"
	"github.com/mynameis-nigel/ssh-bulletin/internal/config"
	"github.com/mynameis-nigel/ssh-bulletin/internal/identity"
	applog "github.com/mynameis-nigel/ssh-bulletin/internal/log"
	"github.com/mynameis-nigel/ssh-bulletin/internal/server"
	"github.com/mynameis-nigel/ssh-bulletin/internal/store"
)

// Run starts the server and exits nonzero on failure.
func Run() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		return 1
	}
	logger := applog.New(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		logger.Error("store open failed", "path", cfg.DBPath, "error", err)
		return 1
	}
	defer st.Close()

	proxyKeys, err := identity.LoadProxyKeys(cfg.ProxyKeysPath)
	if err != nil {
		logger.Error("proxy keys load failed", "path", cfg.ProxyKeysPath, "error", err)
		return 1
	}
	if len(proxyKeys) == 0 {
		logger.Warn("no proxy keys configured: direct connections only (dev mode)")
	}

	svc := board.NewService(st, board.NewBroker(), time.Now)
	if cfg.OwnerFingerprint != "" {
		if err := svc.ConfigureOwner(ctx, cfg.OwnerFingerprint); err != nil {
			logger.Error("BULLETIN_OWNER_FINGERPRINT", "error", err)
			return 1
		}
	}
	if cfg.DevMode {
		if err := board.SeedDemo(ctx, svc, st); err != nil {
			logger.Error("demo seed failed", "error", err)
			return 1
		}
		logger.Warn("dev mode: demo content seeded into an empty database", "database", cfg.DBPath)
	}
	if err := svc.SeedWelcome(ctx); err != nil {
		logger.Error("welcome thread setup failed", "error", err)
		return 1
	}

	srv, err := server.New(cfg, logger, svc, identity.NewResolver(proxyKeys))
	if err != nil {
		logger.Error("server init failed", "error", err)
		return 1
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(done)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			logger.Error("listen failed", "error", err)
			done <- syscall.SIGTERM
		}
	}()

	sig := <-done
	logger.Info("shutdown signal received", "signal", sig.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		logger.Error("shutdown failed", "error", err)
		return 1
	}
	logger.Info("shutdown complete")
	return 0
}
