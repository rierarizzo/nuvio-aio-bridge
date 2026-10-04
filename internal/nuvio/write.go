package nuvio

import "context"

// WatchedKey identifies a watched entry to delete: a movie uses only
// ContentID; an episode adds Season and Episode.
type WatchedKey struct {
	ContentID string `json:"content_id"`
	Season    *int   `json:"season,omitempty"`
	Episode   *int   `json:"episode,omitempty"`
}

// ProgressEntry is a playback progress upsert.
type ProgressEntry struct {
	ContentID   string `json:"content_id"`
	ContentType string `json:"content_type"`
	VideoID     string `json:"video_id"`
	Season      *int   `json:"season,omitempty"`
	Episode     *int   `json:"episode,omitempty"`
	Position    int64  `json:"position"`
	Duration    int64  `json:"duration"`
	LastWatched int64  `json:"last_watched"`
}

// AddToLibrary merges one item into the library and pushes the full snapshot
// back. Nuvio replaces the library wholesale, so unrelated remote items are
// preserved by reading first.
func (c *Client) AddToLibrary(ctx context.Context, item LibraryItem) error {
	items, err := c.Library(ctx)
	if err != nil {
		return err
	}
	existing := make([]LibraryItem, 0, len(items)+1)
	found := false
	for _, current := range items {
		if current.ContentID == item.ContentID {
			existing = append(existing, item)
			found = true
			continue
		}
		existing = append(existing, current)
	}
	if !found {
		existing = append(existing, item)
	}
	return c.pushLibrary(ctx, existing)
}

// RemoveFromLibrary removes one content id and pushes the rest back.
func (c *Client) RemoveFromLibrary(ctx context.Context, contentID string) error {
	items, err := c.Library(ctx)
	if err != nil {
		return err
	}
	kept := make([]LibraryItem, 0, len(items))
	for _, current := range items {
		if current.ContentID == contentID {
			continue
		}
		kept = append(kept, current)
	}
	return c.pushLibrary(ctx, kept)
}

// pushLibrary sends content fields only; the server manages the rest.
func (c *Client) pushLibrary(ctx context.Context, items []LibraryItem) error {
	trimmed := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"content_id":   item.ContentID,
			"content_type": item.ContentType,
		}
		setIfNotEmpty(row, "name", item.Name)
		setIfNotEmpty(row, "poster", item.Poster)
		setIfNotEmpty(row, "poster_shape", item.PosterShape)
		setIfNotEmpty(row, "background", item.Background)
		setIfNotEmpty(row, "description", item.Description)
		setIfNotEmpty(row, "release_info", item.ReleaseInfo)
		if item.IMDBRating != nil {
			row["imdb_rating"] = *item.IMDBRating
		}
		if len(item.Genres) > 0 {
			row["genres"] = item.Genres
		}
		setIfNotEmpty(row, "addon_base_url", item.AddonBase)
		if item.AddedAt > 0 {
			row["added_at"] = item.AddedAt
		}
		trimmed = append(trimmed, row)
	}
	_, err := c.rpc(ctx, "sync_push_library", map[string]any{
		"p_profile_id": c.profile,
		"p_items":      trimmed,
	})
	return err
}

func setIfNotEmpty(row map[string]any, key, value string) {
	if value != "" {
		row[key] = value
	}
}

// MarkWatched upserts one watched entry.
func (c *Client) MarkWatched(ctx context.Context, item WatchedItem) error {
	row := map[string]any{
		"content_id":   item.ContentID,
		"content_type": item.ContentType,
		"watched_at":   item.WatchedAt,
	}
	setIfNotEmpty(row, "title", item.Title)
	if item.Season != nil {
		row["season"] = *item.Season
	}
	if item.Episode != nil {
		row["episode"] = *item.Episode
	}
	_, err := c.rpc(ctx, "sync_push_watched_items", map[string]any{
		"p_profile_id": c.profile,
		"p_items":      []map[string]any{row},
	})
	return err
}

// DeleteWatched removes one watched entry.
func (c *Client) DeleteWatched(ctx context.Context, key WatchedKey) error {
	_, err := c.rpc(ctx, "sync_delete_watched_items", map[string]any{
		"p_profile_id": c.profile,
		"p_keys":       []WatchedKey{key},
	})
	return err
}

// SetProgress upserts a playback progress entry.
func (c *Client) SetProgress(ctx context.Context, entry ProgressEntry) error {
	row := map[string]any{
		"content_id":   entry.ContentID,
		"content_type": entry.ContentType,
		"video_id":     entry.VideoID,
		"position":     entry.Position,
		"duration":     entry.Duration,
		"last_watched": entry.LastWatched,
	}
	if entry.Season != nil {
		row["season"] = *entry.Season
	}
	if entry.Episode != nil {
		row["episode"] = *entry.Episode
	}
	_, err := c.rpc(ctx, "sync_push_watch_progress", map[string]any{
		"p_profile_id": c.profile,
		"p_entries":    []map[string]any{row},
	})
	return err
}

// DeleteProgress removes one progress entry by its progress key.
func (c *Client) DeleteProgress(ctx context.Context, progressKey string) error {
	_, err := c.rpc(ctx, "sync_delete_watch_progress", map[string]any{
		"p_profile_id":   c.profile,
		"p_progress_key": progressKey,
	})
	return err
}
