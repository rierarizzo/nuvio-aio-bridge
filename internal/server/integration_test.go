package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

// fakeNuvio is a stateful stand-in for the Nuvio Supabase API. It lets the
// tests run the real nuvio client and the real bridge handler together, which
// the unit tests (with hand-written doubles) do not cover.
type fakeNuvio struct {
	mu       sync.Mutex
	library  []map[string]any
	watched  []map[string]any
	progress []map[string]any
}

func newFakeNuvio() *fakeNuvio { return &fakeNuvio{} }

func (f *fakeNuvio) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"access_token": "a", "refresh_token": "r", "expires_in": 3600})
	})
	mux.HandleFunc("/rest/v1/rpc/", f.rpc)
	return mux
}

func (f *fakeNuvio) rpc(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var body map[string]json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch strings.TrimPrefix(r.URL.Path, "/rest/v1/rpc/") {
	case "sync_pull_profiles":
		writeJSON(w, []map[string]any{{"profile_index": 1, "name": "Test"}})
	case "sync_pull_library":
		writeJSON(w, f.library)
	case "sync_pull_watched_items":
		writeJSON(w, f.watched)
	case "sync_pull_watch_progress":
		writeJSON(w, f.progress)
	case "sync_push_library":
		var items []map[string]any
		_ = json.Unmarshal(body["p_items"], &items)
		f.library = items
		w.WriteHeader(http.StatusNoContent)
	case "sync_push_watched_items":
		var items []map[string]any
		_ = json.Unmarshal(body["p_items"], &items)
		f.upsertWatched(items)
		w.WriteHeader(http.StatusNoContent)
	case "sync_delete_watched_items":
		var keys []map[string]any
		_ = json.Unmarshal(body["p_keys"], &keys)
		f.deleteWatched(keys)
		w.WriteHeader(http.StatusNoContent)
	case "sync_push_watch_progress":
		var entries []map[string]any
		_ = json.Unmarshal(body["p_entries"], &entries)
		f.upsertProgress(entries)
		w.WriteHeader(http.StatusNoContent)
	case "sync_delete_watch_progress":
		var key string
		_ = json.Unmarshal(body["p_progress_key"], &key)
		f.deleteProgress(key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unknown rpc", http.StatusNotFound)
	}
}

func (f *fakeNuvio) upsertWatched(items []map[string]any) {
	for _, item := range items {
		key := watchedKeyOf(item)
		replaced := false
		for i, existing := range f.watched {
			if watchedKeyOf(existing) == key {
				f.watched[i] = item
				replaced = true
				break
			}
		}
		if !replaced {
			f.watched = append(f.watched, item)
		}
	}
}

func (f *fakeNuvio) deleteWatched(keys []map[string]any) {
	drop := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		drop[watchedKeyOf(key)] = struct{}{}
	}
	kept := make([]map[string]any, 0, len(f.watched))
	for _, item := range f.watched {
		if _, gone := drop[watchedKeyOf(item)]; !gone {
			kept = append(kept, item)
		}
	}
	f.watched = kept
}

func (f *fakeNuvio) upsertProgress(entries []map[string]any) {
	for _, entry := range entries {
		key := progressKeyOf(entry)
		entry["progress_key"] = key
		replaced := false
		for i, existing := range f.progress {
			if existing["progress_key"] == key {
				f.progress[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			f.progress = append(f.progress, entry)
		}
	}
}

func (f *fakeNuvio) deleteProgress(key string) {
	kept := make([]map[string]any, 0, len(f.progress))
	for _, entry := range f.progress {
		if entry["progress_key"] != key {
			kept = append(kept, entry)
		}
	}
	f.progress = kept
}

func watchedKeyOf(row map[string]any) string {
	return fmt.Sprintf("%v|%s|%s", row["content_id"], numKey(row["season"]), numKey(row["episode"]))
}

func progressKeyOf(entry map[string]any) string {
	id := fmt.Sprint(entry["content_id"])
	if entry["season"] == nil || entry["episode"] == nil {
		return id
	}
	return id + "_s" + numKey(entry["season"]) + "e" + numKey(entry["episode"])
}

func numKey(value any) string {
	if value == nil {
		return ""
	}
	if number, ok := value.(float64); ok {
		return strconv.Itoa(int(number))
	}
	return fmt.Sprint(value)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// newBridge wires the real bridge handler to a real Nuvio client pointed at the
// fake server.
func newBridge(t *testing.T, fake *fakeNuvio) *Server {
	t.Helper()
	nuvioTS := httptest.NewServer(fake.handler())
	t.Cleanup(nuvioTS.Close)
	client := nuvio.New(nuvioTS.URL, "anon", "user@example.com", "pass", 1)
	return New("secret", client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func postPush(t *testing.T, srv *Server, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/secret/watch_state/push/series/x.json", strings.NewReader(body)))
	return rec.Code
}

type pullResponse struct {
	Watchlist []struct {
		Type   string `json:"type"`
		MetaID string `json:"metaId"`
	} `json:"watchlist"`
	Watched struct {
		Movies   []string `json:"movies"`
		Episodes []string `json:"episodes"`
		Counts   map[string]struct {
			Watched int   `json:"watched"`
			At      int64 `json:"at"`
		} `json:"counts"`
	} `json:"watched"`
	Items []struct {
		MetaID  string `json:"metaId"`
		VideoID string `json:"videoId"`
	} `json:"items"`
}

func getPull(t *testing.T, srv *Server) pullResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("pull status = %d", rec.Code)
	}
	var out pullResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestEndToEndWatchlistedFromPath(t *testing.T) {
	fake := newFakeNuvio()
	srv := newBridge(t, fake)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/secret/watch_state/push/movie/tt0137523.json",
		strings.NewReader(`{"id":"w1","event":"watchlisted","at":1700000000}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("watchlisted status = %d", rec.Code)
	}

	p := getPull(t, srv)
	if len(p.Watchlist) != 1 || p.Watchlist[0].MetaID != "tt0137523" || p.Watchlist[0].Type != "movie" {
		t.Fatalf("watchlist = %+v", p.Watchlist)
	}
}

func TestEndToEndBulkMarksAndDates(t *testing.T) {
	fake := newFakeNuvio()
	srv := newBridge(t, fake)

	body := `{"id":"b|tt0168366:2|played|1700000000|1","event":"played","scope":"season",` +
		`"at":1700000000,"metaId":"tt0168366","season":2,` +
		`"videos":[{"videoId":"tt0168366:2:1","season":2,"episode":1},` +
		`{"videoId":"tt0168366:2:2","season":2,"episode":2}],"part":1,"parts":1}`
	if code := postPush(t, srv, body); code != http.StatusNoContent {
		t.Fatalf("bulk played status = %d", code)
	}

	p := getPull(t, srv)
	if len(p.Watched.Episodes) != 2 ||
		!contains(p.Watched.Episodes, "tt0168366:2:1") ||
		!contains(p.Watched.Episodes, "tt0168366:2:2") {
		t.Fatalf("episodes = %v", p.Watched.Episodes)
	}
	count, ok := p.Watched.Counts["tt0168366"]
	if !ok || count.Watched != 2 || count.At != 1700000000 {
		t.Fatalf("counts = %+v", p.Watched.Counts)
	}

	// Unplaying the season removes both, and the counts entry goes with them.
	unplayed := strings.ReplaceAll(strings.ReplaceAll(body, `"played"`, `"unplayed"`), `|played|`, `|unplayed|`)
	if code := postPush(t, srv, unplayed); code != http.StatusNoContent {
		t.Fatalf("bulk unplayed status = %d", code)
	}
	p = getPull(t, srv)
	if len(p.Watched.Episodes) != 0 {
		t.Fatalf("episodes after unplayed = %v", p.Watched.Episodes)
	}
	if _, ok := p.Watched.Counts["tt0168366"]; ok {
		t.Fatalf("counts after unplayed = %+v", p.Watched.Counts)
	}
}

func TestEndToEndProgressBecomesItem(t *testing.T) {
	fake := newFakeNuvio()
	srv := newBridge(t, fake)

	body := `{"id":"s1","event":"start","scope":"episode","metaId":"tt0168366",` +
		`"videoId":"tt0168366:2:1","season":2,"episode":1,` +
		`"positionMs":1000,"durationMs":2000,"at":1700000000}`
	if code := postPush(t, srv, body); code != http.StatusNoContent {
		t.Fatalf("start status = %d", code)
	}

	p := getPull(t, srv)
	if len(p.Items) != 1 || p.Items[0].VideoID != "tt0168366:2:1" {
		t.Fatalf("items = %+v", p.Items)
	}
}

func TestEndToEndPullFiltersNuvioState(t *testing.T) {
	fake := newFakeNuvio()
	fake.library = []map[string]any{
		{"content_id": "tt0137523", "content_type": "movie", "name": "Fight Club", "added_at": 1700000000000},
		{"content_id": "streamed:match", "content_type": "sport", "name": "Match"},
	}
	fake.watched = []map[string]any{
		{"content_id": "tt0137523", "content_type": "movie", "watched_at": 1700000000000},
		{"content_id": "tt0903747", "content_type": "series", "season": 3, "episode": 7, "watched_at": 1700000001000},
		{"content_id": "tt0903747", "content_type": "series", "watched_at": 1700000001000}, // show-level, dropped
		{"content_id": "streamed:match", "content_type": "sport", "watched_at": 1700000002000},
	}
	fake.progress = []map[string]any{
		{"content_id": "tt0903747", "content_type": "series", "video_id": "tt0903747:3:7",
			"season": 3, "episode": 7, "position": 100, "duration": 1000,
			"last_watched": 1700000001000, "progress_key": "tt0903747_s3e7"},
		{"content_id": "tt0903747", "content_type": "series", "video_id": "tt0903747:3:8",
			"season": 3, "episode": 8, "position": 1000, "duration": 1000,
			"last_watched": 1700000002000, "progress_key": "tt0903747_s3e8"}, // finished, dropped from items
	}
	srv := newBridge(t, fake)

	p := getPull(t, srv)
	if len(p.Watchlist) != 1 || p.Watchlist[0].MetaID != "tt0137523" {
		t.Fatalf("watchlist = %+v", p.Watchlist)
	}
	if len(p.Watched.Movies) != 1 || p.Watched.Movies[0] != "tt0137523" {
		t.Fatalf("movies = %v", p.Watched.Movies)
	}
	if len(p.Watched.Episodes) != 1 || p.Watched.Episodes[0] != "tt0903747:3:7" {
		t.Fatalf("episodes = %v", p.Watched.Episodes)
	}
	if len(p.Items) != 1 || p.Items[0].VideoID != "tt0903747:3:7" {
		t.Fatalf("items = %+v", p.Items)
	}
}
