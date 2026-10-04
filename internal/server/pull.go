package server

import (
	"encoding/json"
	"net/http"

	"github.com/keneth/nuvio-aio-bridge/internal/pull"
)

// handlePull answers with Nuvio's current watchlist and watched history.
//
// `watchlist` and `watched` are complete lists: on any read failure the field
// is omitted rather than sent empty, because an empty list would delete what
// AIOStreams already imported. `version` is stable across calls, so a matching
// `since` skips re-sending the unchanged watched half.
func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	since := r.URL.Query().Get("since")

	library, err := s.nuvio.Library(ctx)
	if err != nil {
		s.log.Error("pull: library read failed", "err", err)
		http.Error(w, "nuvio library read failed", http.StatusBadGateway)
		return
	}

	watchlist, watched, version := pull.Build(library, nil)

	// The watched half needs a second call. If it fails, omit it (no info)
	// instead of sending an empty list that would wipe imported history.
	watchedOK := true
	rows, err := s.nuvio.Watched(ctx)
	if err != nil {
		s.log.Error("pull: watched read failed", "err", err)
		watchedOK = false
	} else {
		watchlist, watched, version = pull.Build(library, rows)
	}

	payload := pull.Payload{Watchlist: watchlist}
	if since != version {
		if watchedOK {
			w := watched
			payload.Watched = &w
		}
	}
	payload.Version = version

	// `items` is small, changes constantly and is never gated by `version`.
	// A failed read omits it rather than sending an empty list.
	if progress, err := s.nuvio.Progress(ctx, 0); err != nil {
		s.log.Error("pull: progress read failed", "err", err)
	} else {
		payload.Items = pull.BuildItems(progress)
	}

	s.log.Info("pull served",
		"since", since,
		"version", version,
		"watchlist", len(payload.Watchlist),
		"watched_sent", payload.Watched != nil,
		"items", len(payload.Items),
	)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.log.Error("pull: encode", "err", err)
	}
}
