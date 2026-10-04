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
	"github.com/keneth/nuvio-aio-bridge/internal/metadata"
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

	// TMDB first, so artwork matches Nuvio's own; Cinemeta covers the rest.
	cinemeta := metadata.New(cfg.MetadataURL)
	var resolver metadata.Resolver = cinemeta
	if cfg.TMDBAPIKey != "" {
		logger.Info("metadata resolver", "tmdb", true)
		resolver = metadata.NewChain(metadata.NewTMDB(cfg.TMDBAPIKey, cfg.TMDBBaseURL), cinemeta)
	} else {
		logger.Info("metadata resolver", "tmdb", false)
	}

	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           server.New(cfg.BridgeToken, nuvioClient, resolver, logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// AIOStreams cuts a request at 15s, and a handler may wait on Nuvio,
		// so the read and write budgets stay above that to avoid closing a
		// request the caller still considers valid. IdleTimeout only applies
		// between keep-alive requests.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
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
