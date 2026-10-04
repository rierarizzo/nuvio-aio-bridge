// Package metadata resolves title metadata for a Nuvio library entry.
//
// AIOStreams' watchlisted event carries only the id, and Nuvio stores exactly
// what it is given, so without this a favourite added from AIOStreams would
// land in Nuvio with no name or artwork. Cinemeta is used because it needs no
// key and is the metadata source Stremio clients already rely on.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the Cinemeta v3 endpoint.
const DefaultBaseURL = "https://v3-cinemeta.strem.io"

// Meta is the subset of a title's metadata Nuvio stores.
type Meta struct {
	Name        string
	Poster      string
	Background  string
	Description string
	Genres      []string
	ReleaseInfo string
	IMDBRating  float64
}

// Client resolves metadata, caching results in memory.
type Client struct {
	baseURL string
	http    *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	meta Meta
	ok   bool
	at   time.Time
}

const cacheTTL = 24 * time.Hour

// New builds a resolver. An empty baseURL uses the default.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
		cache:   make(map[string]cacheEntry),
	}
}

type cinemetaResponse struct {
	Meta struct {
		Name        string   `json:"name"`
		Poster      string   `json:"poster"`
		Background  string   `json:"background"`
		Description string   `json:"description"`
		Genres      []string `json:"genres"`
		ReleaseInfo string   `json:"releaseInfo"`
		IMDBRating  string   `json:"imdbRating"`
	} `json:"meta"`
}

// Resolve returns metadata for an IMDb id. The bool is false when the title is
// unknown to the source; the caller then writes the id alone.
func (c *Client) Resolve(ctx context.Context, mediaType, imdbID string) (Meta, bool) {
	key := mediaType + "|" + imdbID
	c.mu.Lock()
	if entry, ok := c.cache[key]; ok && time.Since(entry.at) < cacheTTL {
		c.mu.Unlock()
		return entry.meta, entry.ok
	}
	c.mu.Unlock()

	meta, ok := c.fetch(ctx, mediaType, imdbID)

	c.mu.Lock()
	c.cache[key] = cacheEntry{meta: meta, ok: ok, at: time.Now()}
	c.mu.Unlock()
	return meta, ok
}

func (c *Client) fetch(ctx context.Context, mediaType, imdbID string) (Meta, bool) {
	url := fmt.Sprintf("%s/meta/%s/%s.json", c.baseURL, mediaType, imdbID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Meta{}, false
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Meta{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Meta{}, false
	}

	var payload cinemetaResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Meta{}, false
	}
	if payload.Meta.Name == "" {
		return Meta{}, false
	}

	meta := Meta{
		Name:        payload.Meta.Name,
		Poster:      upgradeMetahub(payload.Meta.Poster, "poster"),
		Background:  upgradeMetahub(payload.Meta.Background, "background"),
		Description: payload.Meta.Description,
		Genres:      payload.Meta.Genres,
		ReleaseInfo: payload.Meta.ReleaseInfo,
	}
	if rating, err := parseFloat(payload.Meta.IMDBRating); err == nil {
		meta.IMDBRating = rating
	}
	return meta, true
}

// upgradeMetahub asks Metahub for the largest artwork. Cinemeta returns the
// small variant, which looks poor next to Nuvio's TMDB posters.
func upgradeMetahub(url, kind string) string {
	if url == "" {
		return ""
	}
	return strings.ReplaceAll(url, "/"+kind+"/small/", "/"+kind+"/large/")
}

func parseFloat(value string) (float64, error) {
	var out float64
	if value == "" {
		return 0, fmt.Errorf("empty")
	}
	_, err := fmt.Sscanf(value, "%f", &out)
	return out, err
}
