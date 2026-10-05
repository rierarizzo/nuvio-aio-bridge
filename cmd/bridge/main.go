// Command bridge serves the Nuvio watch_state addon for AIOStreams.
package main

import (
	"context"
	"errors"
	"flag"
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
	// The container HEALTHCHECK re-executes the binary with -healthcheck so the
	// distroless image needs no shell or curl.
	healthcheck := flag.Bool("healthcheck", false, "probe the local /healthz endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(probeHealth())
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	nuvioClient := nuvio.New(cfg.NuvioAPIURL, cfg.NuvioAnonKey, cfg.NuvioEmail, cfg.NuvioPassword, cfg.NuvioProfile)

	// Validate the configured profile once, so a wrong profile or rejected
	// credentials fail at startup instead of on the first event. A transient
	// Nuvio error only warns.
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 10*time.Second)
	err = validateProfile(checkCtx, nuvioClient, logger)
	cancelCheck()
	if err != nil {
		logger.Error("nuvio profile validation failed", "err", err)
		os.Exit(1)
	}

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

// profileChecker is the startup validation surface, kept as an interface so it
// can be tested without a Nuvio account.
type profileChecker interface {
	ProfileIndex(ctx context.Context) (int, string, error)
}

// validateProfile checks the configured profile once. A missing profile or
// rejected credentials is a misconfiguration and stops the bridge; any other
// error (network, server) is transient, so it warns and lets the bridge start.
func validateProfile(ctx context.Context, c profileChecker, log *slog.Logger) error {
	index, name, err := c.ProfileIndex(ctx)
	switch {
	case err == nil:
		log.Info("nuvio profile", "index", index, "name", name)
		return nil
	case errors.Is(err, nuvio.ErrAuth), errors.Is(err, nuvio.ErrProfileNotFound):
		return err
	default:
		log.Warn("could not validate nuvio profile at startup; continuing", "err", err)
		return nil
	}
}

// probeHealth queries the local /healthz endpoint for the container
// HEALTHCHECK. It reads PORT directly so it does not need the full (and
// credential-backed) configuration load.
func probeHealth() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(config.DefaultPort)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
