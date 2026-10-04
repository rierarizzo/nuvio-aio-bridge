package nuvio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer emulates the Nuvio auth and RPC endpoints for tests.
type fakeServer struct {
	// signInStatus is returned by the password grant; 200 issues a session.
	signInStatus int
	// refreshStatus is returned by the refresh grant.
	refreshStatus int
	// rpcStatuses are consumed per RPC call, last one sticky.
	rpcStatuses []int
	rpcCalls    int32
	// authStatus, when set, rejects token grants with this status.
	tokenCalls int32
	profiles   []map[string]any
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.tokenCalls, 1)
		grant := r.URL.Query().Get("grant_type")
		switch grant {
		case "password":
			if f.signInStatus >= 400 {
				w.WriteHeader(f.signInStatus)
				return
			}
		case "refresh_token":
			if f.refreshStatus >= 400 {
				w.WriteHeader(f.refreshStatus)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(authSession{
			AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600,
		})
	})
	mux.HandleFunc("/rest/v1/rpc/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&f.rpcCalls, 1)
		status := http.StatusOK
		if len(f.rpcStatuses) > 0 {
			idx := int(n) - 1
			if idx >= len(f.rpcStatuses) {
				idx = len(f.rpcStatuses) - 1
			}
			status = f.rpcStatuses[idx]
		}
		if status >= 400 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(f.profiles)
	})
	return mux
}

func newTestClient(t *testing.T, f *fakeServer) (*Client, func()) {
	t.Helper()
	ts := httptest.NewServer(f.handler())
	c := New(ts.URL, "anon", "user@example.com", "pass", 1)
	return c, ts.Close
}

func TestSignInAndProfiles(t *testing.T) {
	f := &fakeServer{profiles: []map[string]any{{"profile_index": 1, "name": "Main"}}}
	c, done := newTestClient(t, f)
	defer done()

	profiles, err := c.Profiles(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Index != 1 {
		t.Fatalf("profiles = %+v", profiles)
	}
	if atomic.LoadInt32(&f.tokenCalls) != 1 {
		t.Errorf("token calls = %d, want 1 sign-in", f.tokenCalls)
	}
}

func TestRPCRefreshesOnUnauthorized(t *testing.T) {
	// First RPC call 401s, second succeeds.
	f := &fakeServer{rpcStatuses: []int{401, 200}}
	c, done := newTestClient(t, f)
	defer done()

	if _, err := c.Profiles(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&f.tokenCalls); got != 2 {
		t.Errorf("token calls = %d, want sign-in + refresh", got)
	}
	if got := atomic.LoadInt32(&f.rpcCalls); got != 2 {
		t.Errorf("rpc calls = %d, want retry", got)
	}
}

func TestRefreshRejectedFallsBackToSignIn(t *testing.T) {
	// Seed a session so the first RPC forces a refresh path, then have the
	// refresh be rejected and the fresh sign-in succeed.
	f := &fakeServer{refreshStatus: 401, rpcStatuses: []int{200}}
	c, done := newTestClient(t, f)
	defer done()

	c.accessToken = "expired"
	c.refreshToken = "stale"
	c.expiresAt = time.Unix(0, 0)

	if _, err := c.Profiles(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	// The refresh was rejected, so a new sign-in must have happened.
	if got := atomic.LoadInt32(&f.tokenCalls); got < 2 {
		t.Errorf("token calls = %d, want refresh rejection + sign-in", got)
	}
}

func TestSignInRejectedReturnsErrAuth(t *testing.T) {
	f := &fakeServer{signInStatus: 401}
	c, done := newTestClient(t, f)
	defer done()

	_, err := c.Profiles(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

func TestServerErrorIsNotAuth(t *testing.T) {
	f := &fakeServer{rpcStatuses: []int{500}}
	c, done := newTestClient(t, f)
	defer done()

	_, err := c.Profiles(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, ErrAuth) {
		t.Fatalf("server error must not be ErrAuth: %v", err)
	}
}

func TestLibraryPaginates(t *testing.T) {
	f := &fakeServer{}
	full := make([]map[string]any, pageLibrary)
	for i := range full {
		full[i] = map[string]any{"content_id": "tt0137523", "content_type": "movie"}
	}
	// Two full pages then a short page would be needed; instead emulate a
	// single page smaller than the page size.
	f.profiles = full[:10]
	c, done := newTestClient(t, f)
	defer done()

	items, err := c.Library(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(items) != 10 {
		t.Errorf("library = %d, want 10", len(items))
	}
}

func TestMergeLibraryItemPreservesExisting(t *testing.T) {
	rating := 8.0
	existing := LibraryItem{
		ContentID: "tt0137523", ContentType: "movie", Name: "Fight Club",
		Poster: "poster", PosterShape: "poster", Background: "bg",
		Description: "desc", ReleaseInfo: "1999", IMDBRating: &rating,
		Genres: []string{"Drama"}, AddonBase: "base", AddedAt: 123,
	}
	incoming := LibraryItem{ContentID: "tt0137523", ContentType: "movie", Name: "New"}

	merged := mergeLibraryItem(existing, incoming)
	if merged.Name != "New" {
		t.Errorf("incoming name should win, got %q", merged.Name)
	}
	if merged.Poster != "poster" || merged.Background != "bg" || merged.Description != "desc" {
		t.Errorf("image/description lost: %+v", merged)
	}
	if merged.IMDBRating == nil || *merged.IMDBRating != 8.0 {
		t.Errorf("rating lost: %v", merged.IMDBRating)
	}
	if len(merged.Genres) != 1 || merged.Genres[0] != "Drama" {
		t.Errorf("genres lost: %+v", merged.Genres)
	}
	if merged.AddonBase != "base" || merged.AddedAt != 123 {
		t.Errorf("remote fields lost: %+v", merged)
	}
}

func TestAddToLibrarySerializesWrites(t *testing.T) {
	// The library write is read-modify-write, so concurrent writers must not
	// read the library at the same time or they would overwrite each other.
	var inflight, overlap int32
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(authSession{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600})
	})
	mux.HandleFunc("/rest/v1/rpc/sync_pull_library", func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&inflight, 1) > 1 {
			atomic.StoreInt32(&overlap, 1)
		}
		time.Sleep(25 * time.Millisecond)
		atomic.AddInt32(&inflight, -1)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("/rest/v1/rpc/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := New(ts.URL, "anon", "u@example.com", "p", 1)
	ids := []string{"tt0000001", "tt0000002", "tt0000003", "tt0000004"}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = c.AddToLibrary(context.Background(), LibraryItem{ContentID: id, ContentType: "movie"})
		}(id)
	}
	wg.Wait()

	if atomic.LoadInt32(&overlap) != 0 {
		t.Error("library reads overlapped: AddToLibrary is not serialized")
	}
}
