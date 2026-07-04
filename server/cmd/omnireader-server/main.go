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

	"github.com/amwangfan/omnireader/server/internal/config"
	"github.com/amwangfan/omnireader/server/internal/db"
	"github.com/amwangfan/omnireader/server/internal/httpapi"
)

const version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.LoadFromEnv()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := db.OpenAndMigrate(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer conn.Close()

	handler := httpapi.NewHandler(httpapi.BuildInfo{Version: version})
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting OmniReader server", "addr", cfg.Addr, "data_dir", cfg.DataDir, "database", cfg.DatabasePath)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
