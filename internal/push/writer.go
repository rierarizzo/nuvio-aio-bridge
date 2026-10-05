package push

import (
	"context"

	"github.com/keneth/nuvio-aio-bridge/internal/metadata"
	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

// Writer is the subset of the Nuvio client the push handler needs.
type Writer interface {
	AddToLibrary(ctx context.Context, item nuvio.LibraryItem) error
	RemoveFromLibrary(ctx context.Context, contentID string) error
	MarkWatched(ctx context.Context, item nuvio.WatchedItem) error
	MarkWatchedBatch(ctx context.Context, items []nuvio.WatchedItem) error
	DeleteWatched(ctx context.Context, key nuvio.WatchedKey) error
	DeleteWatchedBatch(ctx context.Context, keys []nuvio.WatchedKey) error
	SetProgress(ctx context.Context, entry nuvio.ProgressEntry) error
	DeleteProgress(ctx context.Context, progressKey string) error
}

// ProgressReader lets a bulk mark clear the resume points it covers. The Nuvio
// client implements it; a writer without it skips that cleanup.
type ProgressReader interface {
	Progress(ctx context.Context, limit int) ([]nuvio.ProgressItem, error)
}

// Resolver fills a library entry's metadata. Nuvio stores only what it is
// given, and the watchlisted event carries only the id.
type Resolver interface {
	Resolve(ctx context.Context, mediaType, imdbID string) (metadata.Meta, bool)
}

// Apply performs the Nuvio write for one event. It returns ok=false when the
// event is understood but carries nothing actionable (for example no IMDb id),
// which the caller should treat as delivered, not as a failure.
//
// resolver may be nil, in which case a favourite is written with the id alone.
func Apply(ctx context.Context, w Writer, resolver Resolver, e Event) (bool, error) {
	if e.IsBulk() {
		return applyBulk(ctx, w, e)
	}

	imdb := e.IMDbID()
	if imdb == "" {
		return false, nil
	}

	switch e.Event {
	case "watchlisted":
		item := nuvio.LibraryItem{
			ContentID:   imdb,
			ContentType: e.MetaType(),
			AddedAt:     e.At * 1000,
		}
		if resolver != nil {
			if meta, ok := resolver.Resolve(ctx, e.MetaType(), imdb); ok {
				item.Name = meta.Name
				item.Poster = meta.Poster
				item.Background = meta.Background
				item.Description = meta.Description
				item.Genres = meta.Genres
				item.ReleaseInfo = meta.ReleaseInfo
				if meta.IMDBRating > 0 {
					rating := meta.IMDBRating
					item.IMDBRating = &rating
				}
			}
		}
		return true, w.AddToLibrary(ctx, item)

	case "unwatchlisted":
		return true, w.RemoveFromLibrary(ctx, imdb)

	case "played":
		return true, markPlayed(ctx, w, e, imdb)

	case "unplayed":
		return true, clearPlayed(ctx, w, e, imdb)

	case "stop":
		if e.Played != nil && *e.Played {
			return true, markPlayed(ctx, w, e, imdb)
		}
		return true, setProgress(ctx, w, e, imdb)

	case "start", "pause":
		// Both carry the current position: `start` is playback or resume,
		// `pause` is where a resume point is kept. Neither writes history.
		return true, setProgress(ctx, w, e, imdb)

	default:
		return false, nil
	}
}

// markPlayed records a watched entry and clears any resume point for the same
// key, so a finished title does not keep a stale position inside Nuvio.
func markPlayed(ctx context.Context, w Writer, e Event, imdb string) error {
	if err := w.MarkWatched(ctx, nuvio.WatchedItem{
		ContentID:   imdb,
		ContentType: e.MetaType(),
		Season:      e.Season,
		Episode:     e.Episode,
		WatchedAt:   e.At * 1000,
	}); err != nil {
		return err
	}
	return deleteProgress(ctx, w, e)
}

// clearPlayed removes the watched mark and the resume point for the same key.
func clearPlayed(ctx context.Context, w Writer, e Event, imdb string) error {
	if err := w.DeleteWatched(ctx, nuvio.WatchedKey{
		ContentID: imdb,
		Season:    e.Season,
		Episode:   e.Episode,
	}); err != nil {
		return err
	}
	return deleteProgress(ctx, w, e)
}

func deleteProgress(ctx context.Context, w Writer, e Event) error {
	key := e.ProgressKey()
	if key == "" {
		return nil
	}
	return w.DeleteProgress(ctx, key)
}

func setProgress(ctx context.Context, w Writer, e Event, imdb string) error {
	return w.SetProgress(ctx, nuvio.ProgressEntry{
		ContentID:   imdb,
		ContentType: e.MetaType(),
		VideoID:     videoID(e, imdb),
		Season:      e.Season,
		Episode:     e.Episode,
		Position:    e.PositionMs,
		Duration:    e.DurationMs,
		LastWatched: e.At * 1000,
	})
}

func videoID(e Event, imdb string) string {
	if e.Scope == "movie" || e.Season == nil || e.Episode == nil {
		return imdb
	}
	return imdb + ":" + itoa(*e.Season) + ":" + itoa(*e.Episode)
}

// applyBulk handles a mark over a whole show or season. AIOStreams sends this
// only when the addon declares `bulk`; without it the same mark arrives as one
// event per video. A large mark is split into independent parts, so returning
// 2xx for one part accepts only that part.
func applyBulk(ctx context.Context, w Writer, e Event) (bool, error) {
	imdb := e.IMDbID()
	if imdb == "" || len(e.Videos) == 0 {
		return false, nil
	}

	switch e.Event {
	case "played":
		items := make([]nuvio.WatchedItem, 0, len(e.Videos))
		for _, v := range e.Videos {
			items = append(items, nuvio.WatchedItem{
				ContentID:   imdb,
				ContentType: "series",
				Season:      v.Season,
				Episode:     v.Episode,
				WatchedAt:   e.At * 1000,
			})
		}
		if err := w.MarkWatchedBatch(ctx, items); err != nil {
			return true, err
		}
	case "unplayed":
		keys := make([]nuvio.WatchedKey, 0, len(e.Videos))
		for _, v := range e.Videos {
			keys = append(keys, nuvio.WatchedKey{
				ContentID: imdb,
				Season:    v.Season,
				Episode:   v.Episode,
			})
		}
		if err := w.DeleteWatchedBatch(ctx, keys); err != nil {
			return true, err
		}
	default:
		return false, nil
	}

	return true, clearProgressForVideos(ctx, w, e, imdb)
}

// clearProgressForVideos removes the resume points a bulk mark covers. It reads
// the small progress list once and deletes only the matching keys, rather than
// one call per video. A failed read is best-effort: the watched write already
// landed, so the mark still counts as applied.
func clearProgressForVideos(ctx context.Context, w Writer, e Event, imdb string) error {
	reader, ok := w.(ProgressReader)
	if !ok {
		return nil
	}
	rows, err := reader.Progress(ctx, 0)
	if err != nil {
		return nil
	}

	var season *int
	if e.Scope == "season" {
		season = e.Season
	}
	for _, row := range rows {
		if row.ContentID != imdb {
			continue
		}
		if season != nil && (row.Season == nil || *row.Season != *season) {
			continue
		}
		key := row.ProgressKey
		if key == "" && row.Season != nil && row.Episode != nil {
			key = imdb + "_s" + itoa(*row.Season) + "e" + itoa(*row.Episode)
		}
		if key == "" {
			continue
		}
		if err := w.DeleteProgress(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
