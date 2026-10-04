package server

import (
	"errors"
	"io"
	"net/http"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
	"github.com/keneth/nuvio-aio-bridge/internal/push"
)

// maxPushBody bounds the event body to avoid unbounded reads.
const maxPushBody = 1 << 20

// handlePush receives a watch_state event and writes it to Nuvio.
//
// Responses follow the AIOStreams contract:
//   - 204: delivered (including events the bridge ignores)
//   - 429/5xx: retry later
//   - other 4xx: drop
//
// A duplicate id is acknowledged without writing again.
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPushBody))
	if err != nil {
		s.log.Warn("push: body read failed", "err", err)
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}

	event, err := push.Parse(body)
	if err != nil {
		s.log.Warn("push: invalid json", "err", err)
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Fall back to path values when the body omits them.
	if event.MetaID == "" {
		event.MetaID = r.PathValue("id")
	}
	if event.Scope == "" {
		event.Scope = r.PathValue("type")
	}

	if !s.dedup.first(event.ID) {
		s.log.Info("push: duplicate ignored", "id", event.ID, "event", event.Event)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ok, err := push.Apply(r.Context(), s.nuvio, s.metadata, event)
	switch {
	case err == nil:
		s.log.Info("push: applied", "id", event.ID, "event", event.Event, "written", ok)
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, nuvio.ErrAuth), errors.Is(err, nuvio.ErrProfileNotFound):
		// The bridge cannot act until the operator fixes the credentials.
		s.log.Error("push: auth problem", "err", err)
		http.Error(w, "auth problem", http.StatusUnauthorized)
	default:
		// Retryable: let AIOStreams back off and try again.
		s.log.Error("push: write failed", "id", event.ID, "event", event.Event, "err", err)
		http.Error(w, "write failed", http.StatusBadGateway)
	}
}
