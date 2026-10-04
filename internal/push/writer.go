package push

import (
	"context"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

// Writer is the subset of the Nuvio client the push handler needs.
type Writer interface {
	AddToLibrary(ctx context.Context, item nuvio.LibraryItem) error
	RemoveFromLibrary(ctx context.Context, contentID string) error
	MarkWatched(ctx context.Context, item nuvio.WatchedItem) error
	DeleteWatched(ctx context.Context, key nuvio.WatchedKey) error
	SetProgress(ctx context.Context, entry nuvio.ProgressEntry) error
	DeleteProgress(ctx context.Context, progressKey string) error
}

// Apply performs the Nuvio write for one event. It returns ok=false when the
// event is understood but carries nothing actionable (for example no IMDb id),
// which the caller should treat as delivered, not as a failure.
func Apply(ctx context.Context, w Writer, e Event) (bool, error) {
	imdb := e.IMDbID()
	if imdb == "" {
		return false, nil
	}

	switch e.Event {
	case "watchlisted":
		return true, w.AddToLibrary(ctx, nuvio.LibraryItem{
			ContentID:   imdb,
			ContentType: e.MetaType(),
			AddedAt:     e.At * 1000,
		})

	case "unwatchlisted":
		return true, w.RemoveFromLibrary(ctx, imdb)

	case "played":
		return true, w.MarkWatched(ctx, nuvio.WatchedItem{
			ContentID:   imdb,
			ContentType: e.MetaType(),
			Season:      e.Season,
			Episode:     e.Episode,
			WatchedAt:   e.At * 1000,
		})

	case "unplayed":
		return true, w.DeleteWatched(ctx, nuvio.WatchedKey{
			ContentID: imdb,
			Season:    e.Season,
			Episode:   e.Episode,
		})

	case "stop":
		if e.Played != nil && *e.Played {
			return true, w.MarkWatched(ctx, nuvio.WatchedItem{
				ContentID:   imdb,
				ContentType: e.MetaType(),
				Season:      e.Season,
				Episode:     e.Episode,
				WatchedAt:   e.At * 1000,
			})
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
