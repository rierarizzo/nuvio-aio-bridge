// Command bridge serves the Nuvio watch_state addon for AIOStreams.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/keneth/nuvio-aio-bridge/internal/config"
	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
	"github.com/keneth/nuvio-aio-bridge/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	nuvioClient := nuvio.New(cfg.NuvioAPIURL, cfg.NuvioAnonKey, cfg.NuvioEmail, cfg.NuvioPassword, cfg.NuvioProfile)

	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           server.New(cfg.BridgeToken, nuvioClient, logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("bridge listening", "port", cfg.Port, "profile", cfg.NuvioProfile)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}
