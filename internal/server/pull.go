package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
	"github.com/keneth/nuvio-aio-bridge/internal/pull"
)

// defaultPullTimeout bounds the whole pull handler. AIOStreams cuts the request
// at 15s, so the bridge gives up earlier and answers instead of being cut off
// mid-response.
const defaultPullTimeout = 12 * time.Second

// handlePull answers with Nuvio's current watchlist, watched history and
// in-progress items.
//
// The three reads run concurrently because each one pages through Nuvio and the
// sum of the sequential calls would otherwise exceed the request budget.
// `watchlist` and `watched` are complete lists: on any read failure the field is
// omitted rather than sent empty, because an empty list would delete what
// AIOStreams already imported. `version` is stable across calls, so a matching
// `since` skips re-sending the unchanged watched half.
func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	timeout := s.pullTimeout
	if timeout <= 0 {
		timeout = defaultPullTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	since := r.URL.Query().Get("since")

	var (
		library    []nuvio.LibraryItem
		libErr     error
		rows       []nuvio.WatchedItem
		watchedErr error
		progress   []nuvio.ProgressItem
		progErr    error
	)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		library, libErr = s.nuvio.Library(ctx)
	}()
	go func() {
		defer wg.Done()
		rows, watchedErr = s.nuvio.Watched(ctx)
	}()
	go func() {
		defer wg.Done()
		progress, progErr = s.nuvio.Progress(ctx, 0)
	}()
	wg.Wait()

	if libErr != nil {
		s.log.Error("pull: library read failed", "err", libErr)
		if errors.Is(libErr, nuvio.ErrAuth) || errors.Is(libErr, nuvio.ErrProfileNotFound) {
			http.Error(w, "auth problem", http.StatusUnauthorized)
			return
		}
		http.Error(w, "nuvio library read failed", http.StatusBadGateway)
		return
	}

	// `items` is small, changes constantly and is never gated by `version`.
	// A failed read omits it rather than sending an empty list.
	var items []pull.Item
	if progErr != nil {
		s.log.Error("pull: progress read failed", "err", progErr)
	} else {
		items = pull.BuildItems(progress)
	}

	watchlist, watched, version := pull.Build(library, nil)

	// The watched half needs a second pass. If it fails, omit it (no info)
	// instead of sending an empty list that would wipe imported history.
	watchedOK := watchedErr == nil
	if watchedOK {
		// Name an in-progress video in `items` alone: AIOStreams imports
		// `items` before `watched` and its watched import clears the resume
		// position of every video it names, which would drop a rewatch from
		// Continue Watching. When the progress read failed, `items` is empty
		// and the watched list is left whole.
		watchlist, watched, version = pull.Build(library, pull.DropInProgress(rows, items))
	} else {
		s.log.Error("pull: watched read failed", "err", watchedErr)
	}

	// The library read succeeded, so the watchlist is authoritative even when
	// empty: send it so a favourite removed in Nuvio disappears in AIOStreams.
	payload := pull.Payload{Watchlist: &watchlist}
	if since != version && watchedOK {
		w := watched
		payload.Watched = &w
	}
	payload.Version = version

	payload.Items = items

	watchlistCount := 0
	if payload.Watchlist != nil {
		watchlistCount = len(*payload.Watchlist)
	}
	s.log.Info("pull served",
		"since", since,
		"version", version,
		"watchlist", watchlistCount,
		"watched_sent", payload.Watched != nil,
		"items", len(payload.Items),
	)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.log.Error("pull: encode", "err", err)
	}
}
