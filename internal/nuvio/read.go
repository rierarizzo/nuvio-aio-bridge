package nuvio

import (
	"context"
	"encoding/json"
)

// LibraryItem is one entry of a profile's library (the watchlist).
type LibraryItem struct {
	ContentID   string   `json:"content_id"`
	ContentType string   `json:"content_type"`
	Name        string   `json:"name"`
	Poster      string   `json:"poster"`
	PosterShape string   `json:"poster_shape"`
	Background  string   `json:"background"`
	Description string   `json:"description"`
	ReleaseInfo string   `json:"release_info"`
	IMDBRating  *float64 `json:"imdb_rating"`
	Genres      []string `json:"genres"`
	AddonBase   string   `json:"addon_base_url"`
	AddedAt     int64    `json:"added_at"`
}

// WatchedItem is one watched entry. A movie has a bare content id; a series
// has the show id plus season and episode.
type WatchedItem struct {
	ContentID   string `json:"content_id"`
	ContentType string `json:"content_type"`
	Title       string `json:"title"`
	Season      *int   `json:"season"`
	Episode     *int   `json:"episode"`
	WatchedAt   int64  `json:"watched_at"`
}

const (
	pageLibrary = 500
	pageWatched = 500
)

// Profiles lists the profiles on the account.
func (c *Client) Profiles(ctx context.Context) ([]Profile, error) {
	raw, err := c.rpc(ctx, "sync_pull_profiles", map[string]any{})
	if err != nil {
		return nil, err
	}
	var profiles []Profile
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// ProfileIndex reports whether the configured profile exists and its name.
func (c *Client) ProfileIndex(ctx context.Context) (int, string, error) {
	profiles, err := c.Profiles(ctx)
	if err != nil {
		return 0, "", err
	}
	for _, p := range profiles {
		if p.Index == c.profile {
			return p.Index, p.Name, nil
		}
	}
	return 0, "", ErrProfileNotFound
}

// Library reads the whole library, paging until a short page.
func (c *Client) Library(ctx context.Context) ([]LibraryItem, error) {
	var out []LibraryItem
	for offset := 0; ; offset += pageLibrary {
		raw, err := c.rpc(ctx, "sync_pull_library", map[string]any{
			"p_profile_id": c.profile,
			"p_limit":      pageLibrary,
			"p_offset":     offset,
		})
		if err != nil {
			return nil, err
		}
		var page []LibraryItem
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < pageLibrary {
			return out, nil
		}
	}
}

// Watched reads the whole watched history, paging until a short page.
func (c *Client) Watched(ctx context.Context) ([]WatchedItem, error) {
	var out []WatchedItem
	for page := 1; ; page++ {
		raw, err := c.rpc(ctx, "sync_pull_watched_items", map[string]any{
			"p_profile_id": c.profile,
			"p_page":       page,
			"p_page_size":  pageWatched,
		})
		if err != nil {
			return nil, err
		}
		var rows []WatchedItem
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < pageWatched {
			return out, nil
		}
	}
}

// Progress reads the in-progress list. It is bounded and unpaginated.
func (c *Client) Progress(ctx context.Context, limit int) ([]ProgressItem, error) {
	if limit <= 0 {
		limit = 200
	}
	raw, err := c.rpc(ctx, "sync_pull_watch_progress", map[string]any{
		"p_profile_id": c.profile,
		"p_limit":      limit,
	})
	if err != nil {
		return nil, err
	}
	var rows []ProgressItem
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
