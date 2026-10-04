package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultTMDBBaseURL is the TMDB API endpoint.
const DefaultTMDBBaseURL = "https://api.themoviedb.org/3"

// TMDB image sizes, matching what Nuvio and Scrob store.
const (
	tmdbPosterSize = "w500"
	tmdbBackdrop   = "w1280"
)

// tmdbMovieGenres and tmdbTVGenres translate TMDB genre ids to names. Movies
// and TV share some ids (18, 35, ...) but not all (Action is 28 for movies and
// 10759 for TV), so the table depends on the media type. The `find` endpoint
// returns only ids, not names.
var tmdbMovieGenres = map[int]string{
	28: "Action", 12: "Adventure", 16: "Animation", 35: "Comedy",
	80: "Crime", 99: "Documentary", 18: "Drama", 10751: "Family",
	14: "Fantasy", 36: "History", 27: "Horror", 10402: "Music",
	9648: "Mystery", 10749: "Romance", 878: "Science Fiction",
	10770: "TV Movie", 53: "Thriller", 10752: "War", 37: "Western",
}

var tmdbTVGenres = map[int]string{
	10759: "Action & Adventure", 16: "Animation", 35: "Comedy",
	80: "Crime", 99: "Documentary", 18: "Drama", 10751: "Family",
	10762: "Kids", 9648: "Mystery", 10763: "News", 10764: "Reality",
	10765: "Sci-Fi & Fantasy", 10766: "Soap", 10767: "Talk",
	10768: "War & Politics", 37: "Western",
}

// TMDB resolves metadata from TMDB, the same source Nuvio itself uses, so a
// favourite added from AIOStreams gets the same artwork as one added in Nuvio.
type TMDB struct {
	apiKey  string
	baseURL string
	http    *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewTMDB builds a TMDB resolver. An empty baseURL uses the default.
func NewTMDB(apiKey, baseURL string) *TMDB {
	if baseURL == "" {
		baseURL = DefaultTMDBBaseURL
	}
	return &TMDB{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
		cache:   make(map[string]cacheEntry),
	}
}

type tmdbFindResponse struct {
	MovieResults []tmdbEntity `json:"movie_results"`
	TVResults    []tmdbEntity `json:"tv_results"`
}

type tmdbEntity struct {
	Name         string  `json:"name"`
	Title        string  `json:"title"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	VoteAverage  float64 `json:"vote_average"`
	GenreIDs     []int   `json:"genre_ids"`
}

// Resolve looks a title up by IMDb id and returns Nuvio-ready metadata.
func (t *TMDB) Resolve(ctx context.Context, mediaType, imdbID string) (Meta, bool) {
	if t.apiKey == "" {
		return Meta{}, false
	}
	key := "tmdb|" + mediaType + "|" + imdbID
	t.mu.Lock()
	if entry, ok := t.cache[key]; ok && time.Since(entry.at) < cacheTTL {
		t.mu.Unlock()
		return entry.meta, entry.ok
	}
	t.mu.Unlock()

	meta, ok := t.fetch(ctx, mediaType, imdbID)

	t.mu.Lock()
	t.cache[key] = cacheEntry{meta: meta, ok: ok, at: time.Now()}
	t.mu.Unlock()
	return meta, ok
}

func (t *TMDB) fetch(ctx context.Context, mediaType, imdbID string) (Meta, bool) {
	params := url.Values{}
	params.Set("external_source", "imdb_id")
	params.Set("api_key", t.apiKey)
	endpoint := fmt.Sprintf("%s/find/%s?%s", t.baseURL, url.PathEscape(imdbID), params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Meta{}, false
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return Meta{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Meta{}, false
	}

	var payload tmdbFindResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Meta{}, false
	}

	results := payload.MovieResults
	if mediaType == "series" {
		results = payload.TVResults
	}
	if len(results) == 0 {
		return Meta{}, false
	}
	entity := results[0]
	if entity.PosterPath == "" && entity.Overview == "" {
		return Meta{}, false
	}

	name := entity.Title
	if mediaType == "series" {
		name = entity.Name
	}
	release := entity.ReleaseDate
	if mediaType == "series" {
		release = entity.FirstAirDate
	}

	meta := Meta{
		Name:        name,
		Description: entity.Overview,
		ReleaseInfo: year(release),
		IMDBRating:  entity.VoteAverage,
	}
	if entity.PosterPath != "" {
		meta.Poster = tmdbImage(entity.PosterPath, tmdbPosterSize)
	}
	if entity.BackdropPath != "" {
		meta.Background = tmdbImage(entity.BackdropPath, tmdbBackdrop)
	}
	meta.Genres = tmdbGenreNames(mediaType, entity.GenreIDs)
	return meta, true
}

func tmdbImage(path, size string) string {
	return fmt.Sprintf("https://image.tmdb.org/t/p/%s%s", size, path)
}

func year(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return date
}

// tmdbGenreNames maps genre ids to names, preserving order and dropping
// unknown ids rather than guessing. It returns an empty slice on no match.
func tmdbGenreNames(mediaType string, ids []int) []string {
	table := tmdbMovieGenres
	if mediaType == "series" {
		table = tmdbTVGenres
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		name, ok := table[id]
		if !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
