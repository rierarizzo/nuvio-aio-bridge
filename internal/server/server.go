// Package server wires the HTTP routes the bridge exposes to AIOStreams.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

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
	token string
	nuvio NuvioClient
	log   *slog.Logger
	dedup *deduper
}

// New returns a server guarding its routes with token.
func New(token string, client NuvioClient, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{token: token, nuvio: client, log: logger, dedup: newDeduper(4096)}
}

// Handler returns the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	prefix := "/" + s.token

	mux.HandleFunc("GET "+prefix+"/manifest.json", s.handleManifest)
	mux.HandleFunc("POST "+prefix+"/watch_state/push/{type}/{id}", s.handlePush)
	mux.HandleFunc("GET "+prefix+"/watch_state/pull.json", s.handlePull)

	return mux
}

func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(manifest.Build()); err != nil {
		s.log.Error("encode manifest", "err", err)
	}
}
