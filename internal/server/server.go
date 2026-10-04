// Package server wires the HTTP routes the bridge exposes to AIOStreams.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/keneth/nuvio-aio-bridge/internal/manifest"
	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
	"github.com/keneth/nuvio-aio-bridge/internal/push"
)

// NuvioClient is the full Nuvio surface the server uses: reads for the pull,
// writes for the push.
type NuvioClient interface {
	push.Writer
	Library(ctx context.Context) ([]nuvio.LibraryItem, error)
	Watched(ctx context.Context) ([]nuvio.WatchedItem, error)
	Progress(ctx context.Context, limit int) ([]nuvio.ProgressItem, error)
}

// Server holds the dependencies shared by the handlers.
type Server struct {
	token       string
	nuvio       NuvioClient
	metadata    push.Resolver
	log         *slog.Logger
	dedup       *deduper
	pullTimeout time.Duration
}

// New returns a server guarding its routes with token. A nil metadata
// resolver leaves favourites written with the id alone.
func New(token string, client NuvioClient, resolver push.Resolver, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		token:       token,
		nuvio:       client,
		metadata:    resolver,
		log:         logger,
		dedup:       newDeduper(4096),
		pullTimeout: defaultPullTimeout,
	}
}

// Handler returns the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	prefix := "/" + s.token

	mux.HandleFunc("GET "+prefix+"/manifest.json", s.handleManifest)
	mux.HandleFunc("POST "+prefix+"/watch_state/push/{type}/{id}", s.handlePush)
	mux.HandleFunc("GET "+prefix+"/watch_state/pull.json", s.handlePull)
	// Liveness probe for the container; it deliberately has no token so a
	// local check does not need the addon secret.
	mux.HandleFunc("GET /healthz", s.handleHealth)

	return mux
}

// handleHealth reports that the process is up. It does not touch Nuvio, so it
// stays cheap and does not turn an upstream outage into a restart loop.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(manifest.Build()); err != nil {
		s.log.Error("encode manifest", "err", err)
	}
}
