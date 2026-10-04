package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/keneth/nuvio-aio-bridge/internal/manifest"
	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
	"github.com/keneth/nuvio-aio-bridge/internal/push"
)

type fakeClient struct {
	library  []nuvio.LibraryItem
	watched  []nuvio.WatchedItem
	progress []nuvio.ProgressItem
	libErr   error
	wErr     error
	progErr  error

	writeCalls []string
	writeErr   error
}

func (f *fakeClient) Library(context.Context) ([]nuvio.LibraryItem, error) {
	return f.library, f.libErr
}

func (f *fakeClient) Watched(context.Context) ([]nuvio.WatchedItem, error) {
	return f.watched, f.wErr
}

func (f *fakeClient) Progress(context.Context, int) ([]nuvio.ProgressItem, error) {
	return f.progress, f.progErr
}

func (f *fakeClient) AddToLibrary(context.Context, nuvio.LibraryItem) error {
	f.writeCalls = append(f.writeCalls, "add")
	return f.writeErr
}

func (f *fakeClient) RemoveFromLibrary(context.Context, string) error {
	f.writeCalls = append(f.writeCalls, "remove")
	return f.writeErr
}

func (f *fakeClient) MarkWatched(context.Context, nuvio.WatchedItem) error {
	f.writeCalls = append(f.writeCalls, "mark")
	return f.writeErr
}

func (f *fakeClient) DeleteWatched(context.Context, nuvio.WatchedKey) error {
	f.writeCalls = append(f.writeCalls, "unmark")
	return f.writeErr
}

func (f *fakeClient) SetProgress(context.Context, nuvio.ProgressEntry) error {
	f.writeCalls = append(f.writeCalls, "progress")
	return f.writeErr
}

func (f *fakeClient) DeleteProgress(context.Context, string) error {
	f.writeCalls = append(f.writeCalls, "delprogress")
	return f.writeErr
}

var _ push.Writer = (*fakeClient)(nil)

func TestManifestDeclaresProgressEvents(t *testing.T) {
	events := strings.Join(manifest.Build().WatchState.Push.Events, ",")
	for _, want := range []string{"start", "pause", "stop", "played", "unplayed", "watchlisted", "unwatchlisted"} {
		if !strings.Contains(events, want) {
			t.Errorf("manifest events missing %q: %s", want, events)
		}
	}
}

func TestManifestRequiresToken(t *testing.T) {
	srv := New("secret", &fakeClient{}, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wrong/manifest.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for wrong token, got %d", rec.Code)
	}
}

func TestManifestServed(t *testing.T) {
	srv := New("secret", &fakeClient{}, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/manifest.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var got manifest.Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if got.WatchState.Version != 2 {
		t.Errorf("watchState.version = %d", got.WatchState.Version)
	}
}

func TestPullBuildsWatchlistAndWatched(t *testing.T) {
	reader := &fakeClient{
		library: []nuvio.LibraryItem{
			{ContentID: "tt0137523", ContentType: "movie", AddedAt: 100},
			{ContentID: "tt0903747", ContentType: "series", AddedAt: 200},
			{ContentID: "streamed:x", ContentType: "sport", AddedAt: 300},
		},
		watched: []nuvio.WatchedItem{
			{ContentID: "tt0137523", ContentType: "movie"},
			{ContentID: "tt0903747", ContentType: "series", Season: ptr(3), Episode: ptr(7)},
			{ContentID: "tt0903747", ContentType: "series"},
		},
	}
	srv := New("secret", reader, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var payload struct {
		Version string `json:"version"`
		Watched *struct {
			Movies   []string `json:"movies"`
			Episodes []string `json:"episodes"`
		} `json:"watched"`
		Watchlist []struct {
			Type   string `json:"type"`
			MetaID string `json:"metaId"`
		} `json:"watchlist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if len(payload.Watchlist) != 2 {
		t.Errorf("watchlist = %+v, want 2", payload.Watchlist)
	}
	if payload.Watched == nil || len(payload.Watched.Movies) != 1 || len(payload.Watched.Episodes) != 1 {
		t.Errorf("watched = %+v", payload.Watched)
	}
}

func TestPullOmitsWatchedWhenSinceMatches(t *testing.T) {
	reader := &fakeClient{
		library: []nuvio.LibraryItem{{ContentID: "tt0137523", ContentType: "movie"}},
		watched: []nuvio.WatchedItem{{ContentID: "tt0137523", ContentType: "movie"}},
	}
	srv := New("secret", reader, nil, nil)

	first := httptest.NewRecorder()
	srv.Handler().ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}

	second := httptest.NewRecorder()
	srv.Handler().ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json?since="+v.Version, nil))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["watched"]; ok {
		t.Error("watched should be omitted when since matches")
	}
}

func TestPullOmitsWatchedOnReadError(t *testing.T) {
	reader := &fakeClient{
		library: []nuvio.LibraryItem{{ContentID: "tt0137523", ContentType: "movie"}},
		wErr:    errors.New("boom"),
	}
	srv := New("secret", reader, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["watched"]; ok {
		t.Error("watched must be omitted when the read fails")
	}
}

func TestPushMapsEvents(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"watchlisted", `{"id":"1","event":"watchlisted","scope":"movie","metaId":"tt0137523","at":1}`, "add"},
		{"unwatchlisted", `{"id":"2","event":"unwatchlisted","scope":"movie","metaId":"tt0137523","at":1}`, "remove"},
		{"played", `{"id":"3","event":"played","scope":"series","metaId":"tt0903747","season":3,"episode":7,"at":1}`, "mark"},
		{"unplayed", `{"id":"4","event":"unplayed","scope":"series","metaId":"tt0903747","season":3,"episode":7,"at":1}`, "unmark"},
		{"stop unfinished", `{"id":"5","event":"stop","scope":"movie","metaId":"tt0137523","positionMs":100,"durationMs":1000,"played":false,"at":1}`, "progress"},
		{"stop finished", `{"id":"6","event":"stop","scope":"movie","metaId":"tt0137523","positionMs":1000,"durationMs":1000,"played":true,"at":1}`, "mark"},
		{"start", `{"id":"7","event":"start","scope":"episode","metaId":"tt0903747","season":3,"episode":7,"positionMs":100,"durationMs":1000,"at":1}`, "progress"},
		{"pause", `{"id":"8","event":"pause","scope":"episode","metaId":"tt0903747","season":3,"episode":7,"positionMs":200,"durationMs":1000,"at":1}`, "progress"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{}
			srv := New("secret", client, nil, nil)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/secret/watch_state/push/movie/tt0137523.json", strings.NewReader(tc.body))
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("want 204, got %d", rec.Code)
			}
			if len(client.writeCalls) != 1 || client.writeCalls[0] != tc.want {
				t.Errorf("writeCalls = %v, want [%s]", client.writeCalls, tc.want)
			}
		})
	}
}

func TestPullIncludesItems(t *testing.T) {
	reader := &fakeClient{
		progress: []nuvio.ProgressItem{
			{ContentID: "tt0137523", ContentType: "movie", Position: 100, Duration: 1000, LastWatched: 2000},
			{ContentID: "tt0903747", ContentType: "series", Season: ptr(3), Episode: ptr(7), Position: 200, Duration: 1000, LastWatched: 1000},
			{ContentID: "tt0137524", ContentType: "movie", Position: 1000, Duration: 1000, LastWatched: 3000}, // finished
			{ContentID: "streamed:x", ContentType: "sport", Position: 1, Duration: 2},
			{ContentID: "tt0903747", ContentType: "series", Position: 1, Duration: 2}, // show-level, no episode
		},
	}
	srv := New("secret", reader, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))

	var payload struct {
		Items []struct {
			Type        string  `json:"type"`
			MetaID      string  `json:"metaId"`
			VideoID     string  `json:"videoId"`
			ProgressPct float64 `json:"progressPercent"`
			At          int64   `json:"at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %+v, want 2 (finished/sport/show-level excluded)", payload.Items)
	}
	// Newest first.
	if payload.Items[0].MetaID != "tt0137523" || payload.Items[0].At != 2 {
		t.Errorf("first item = %+v", payload.Items[0])
	}
	if payload.Items[1].VideoID != "tt0903747:3:7" {
		t.Errorf("second item videoId = %q", payload.Items[1].VideoID)
	}
	if payload.Items[0].ProgressPct != 10 {
		t.Errorf("progressPercent = %v", payload.Items[0].ProgressPct)
	}
}

func TestPullReturnsErrorOnLibraryFailure(t *testing.T) {
	srv := New("secret", &fakeClient{libErr: errors.New("nuvio down")}, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502 when the library read fails, got %d", rec.Code)
	}
}

func TestPullOmitsItemsOnProgressError(t *testing.T) {
	reader := &fakeClient{
		library: []nuvio.LibraryItem{{ContentID: "tt0137523", ContentType: "movie"}},
		progErr: errors.New("nuvio down"),
	}
	srv := New("secret", reader, nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/watch_state/pull.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["items"]; ok {
		t.Error("items must be omitted when the progress read fails")
	}
}

func TestPushDeduplicates(t *testing.T) {
	client := &fakeClient{}
	srv := New("secret", client, nil, nil)
	body := `{"id":"dup","event":"watchlisted","scope":"movie","metaId":"tt0137523","at":1}`
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/secret/watch_state/push/movie/tt0137523.json", strings.NewReader(body))
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("want 204, got %d", rec.Code)
		}
	}
	if len(client.writeCalls) != 1 {
		t.Errorf("writeCalls = %v, want a single write", client.writeCalls)
	}
}

func TestPushIgnoresUnknownEvent(t *testing.T) {
	client := &fakeClient{}
	srv := New("secret", client, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secret/watch_state/push/movie/tt0137523.json",
		strings.NewReader(`{"id":"x","event":"dropped","scope":"movie","metaId":"tt0137523"}`))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204 for ignored event, got %d", rec.Code)
	}
	if len(client.writeCalls) != 0 {
		t.Errorf("writeCalls = %v, want none", client.writeCalls)
	}
}

func TestPushReturnsRetryableOnWriteError(t *testing.T) {
	client := &fakeClient{writeErr: errors.New("nuvio down")}
	srv := New("secret", client, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secret/watch_state/push/movie/tt0137523.json",
		strings.NewReader(`{"id":"e","event":"watchlisted","scope":"movie","metaId":"tt0137523"}`))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
}

func TestPushReturnsAuthOnAuthError(t *testing.T) {
	client := &fakeClient{writeErr: nuvio.ErrAuth}
	srv := New("secret", client, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secret/watch_state/push/movie/tt0137523.json",
		strings.NewReader(`{"id":"a","event":"watchlisted","scope":"movie","metaId":"tt0137523"}`))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func ptr(n int) *int { return &n }
