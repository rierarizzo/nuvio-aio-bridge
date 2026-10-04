package metadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubResolver struct {
	meta Meta
	ok   bool
}

func (s stubResolver) Resolve(context.Context, string, string) (Meta, bool) {
	return s.meta, s.ok
}

func TestChainPrefersFirstHit(t *testing.T) {
	first := stubResolver{meta: Meta{Name: "first"}, ok: true}
	second := stubResolver{meta: Meta{Name: "second"}, ok: true}
	meta, ok := NewChain(first, second).Resolve(context.Background(), "movie", "tt1")
	if !ok || meta.Name != "first" {
		t.Fatalf("meta=%+v ok=%v", meta, ok)
	}
}

func TestChainFallsBack(t *testing.T) {
	first := stubResolver{ok: false}
	second := stubResolver{meta: Meta{Name: "second"}, ok: true}
	meta, ok := NewChain(first, second).Resolve(context.Background(), "movie", "tt1")
	if !ok || meta.Name != "second" {
		t.Fatalf("meta=%+v ok=%v", meta, ok)
	}
}

func TestChainAllMiss(t *testing.T) {
	meta, ok := NewChain(stubResolver{}, nil).Resolve(context.Background(), "movie", "tt1")
	if ok || meta.Name != "" {
		t.Fatalf("meta=%+v ok=%v", meta, ok)
	}
}

func TestUpgradeMetahub(t *testing.T) {
	in := "https://images.metahub.space/poster/small/tt1844624/img"
	want := "https://images.metahub.space/poster/large/tt1844624/img"
	if got := upgradeMetahub(in, "poster"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := upgradeMetahub("", "poster"); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
}

func TestTMDBWithoutKeyMisses(t *testing.T) {
	client := NewTMDB("", "")
	if _, ok := client.Resolve(context.Background(), "movie", "tt1"); ok {
		t.Error("TMDB without a key should not resolve")
	}
}

func TestYear(t *testing.T) {
	if got := year("2011-10-05"); got != "2011" {
		t.Errorf("got %q", got)
	}
	if got := year(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTMDBGenreNames(t *testing.T) {
	cases := []struct {
		name      string
		mediaType string
		ids       []int
		want      []string
	}{
		{"movie multi", "movie", []int{18, 878}, []string{"Drama", "Science Fiction"}},
		{"series single", "series", []int{10765}, []string{"Sci-Fi & Fantasy"}},
		{"unknown skipped", "movie", []int{999999}, nil},
		{"duplicates", "movie", []int{18, 18}, []string{"Drama"}},
		{"movie id not used for tv", "series", []int{28}, nil},
		{"tv id not used for movie", "movie", []int{10759}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tmdbGenreNames(tc.mediaType, tc.ids)
			if !sameStrings(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTMDBResolveMapsGenres(t *testing.T) {
	cases := []struct {
		name           string
		mediaType      string
		body           string
		wantName       string
		wantYear       string
		wantPoster     string
		wantBackground string
		want           []string
	}{
		{
			name:      "movie",
			mediaType: "movie",
			body: `{"movie_results":[{"title":"Fight Club","overview":"An insomniac office worker.",` +
				`"poster_path":"/p.jpg","backdrop_path":"/b.jpg","release_date":"1999-10-15",` +
				`"vote_average":8.4,"genre_ids":[18,878]}]}`,
			wantName:       "Fight Club",
			wantYear:       "1999",
			wantPoster:     "https://image.tmdb.org/t/p/w500/p.jpg",
			wantBackground: "https://image.tmdb.org/t/p/w1280/b.jpg",
			want:           []string{"Drama", "Science Fiction"},
		},
		{
			name:      "series",
			mediaType: "series",
			body: `{"tv_results":[{"name":"Breaking Bad","overview":"A chemistry teacher.",` +
				`"poster_path":"/p2.jpg","backdrop_path":"/b2.jpg","first_air_date":"2008-01-20",` +
				`"vote_average":8.9,"genre_ids":[18,10765]}]}`,
			wantName:       "Breaking Bad",
			wantYear:       "2008",
			wantPoster:     "https://image.tmdb.org/t/p/w500/p2.jpg",
			wantBackground: "https://image.tmdb.org/t/p/w1280/b2.jpg",
			want:           []string{"Drama", "Sci-Fi & Fantasy"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer ts.Close()

			meta, ok := NewTMDB("test-key", ts.URL).Resolve(context.Background(), tc.mediaType, "tt0000000")
			if !ok {
				t.Fatal("expected a hit")
			}
			if meta.Name != tc.wantName || meta.ReleaseInfo != tc.wantYear {
				t.Errorf("name=%q year=%q", meta.Name, meta.ReleaseInfo)
			}
			if meta.Poster != tc.wantPoster || meta.Background != tc.wantBackground {
				t.Errorf("poster=%q background=%q", meta.Poster, meta.Background)
			}
			if !sameStrings(meta.Genres, tc.want) {
				t.Errorf("genres = %v, want %v", meta.Genres, tc.want)
			}
		})
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
