package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/atlantis-mk/FastProxy-Server/internal/api"
	"github.com/atlantis-mk/FastProxy-Server/internal/appconfig"
	"github.com/atlantis-mk/FastProxy-Server/internal/appdata"
	"github.com/atlantis-mk/FastProxy-Server/internal/daemon"
	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
	"github.com/atlantis-mk/FastProxy-Server/internal/subsync"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := daemon.Install(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				slog.Error("install FastProxy daemon", "error", err)
				os.Exit(1)
			}
			return
		case "serve":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		}
	}

	cfg := appconfig.Load()
	logger := newLogger(cfg.LogLevel)

	dataDir, err := appdata.Resolve(cfg.DataDir)
	if err != nil {
		logger.Error("resolve app data dir", "error", err)
		os.Exit(1)
	}
	if err := appdata.Ensure(dataDir); err != nil {
		logger.Error("ensure app data dir", "error", err)
		os.Exit(1)
	}
	cfg.DataDir = dataDir

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := repository.NewStore(dataDir)
	if err != nil {
		logger.Error("initialize repository store", "error", err)
		os.Exit(1)
	}

	server := api.NewServer(cfg, logger, store)
	subscriptionSync := subsync.NewService(store, logger)
	go subsync.NewAutoUpdater(store, subscriptionSync, logger).Start(ctx)
	logger.Info("starting FastProxy server", "addr", cfg.Addr, "dataDir", cfg.DataDir)
	if err := api.ListenAndServe(ctx, cfg.Addr, server.Handler()); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
