// Package pull turns Nuvio's library and watched history into the AIOStreams
// watch_state pull payload.
package pull

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

// finishedPercent is the progress at or above which an item counts as
// finished rather than in progress. It matches AIOStreams' played threshold.
const finishedPercent = 90.0

// Payload is the body of `GET /{token}/watch_state/pull.json`.
//
// `watchlist` is a pointer so a successful read that found nothing sends `[]`
// (which clears the favourites) rather than omitting the field (which means
// "no information"). `watched` works the same way.
type Payload struct {
	Version   string      `json:"version"`
	Items     []Item      `json:"items,omitempty"`
	Watched   *Watched    `json:"watched,omitempty"`
	Watchlist *[]WatchRow `json:"watchlist,omitempty"`
}

// Watched is the watched half of the answer.
type Watched struct {
	Movies   []string `json:"movies,omitempty"`
	Episodes []string `json:"episodes,omitempty"`
}

// WatchRow is one watchlist entry.
type WatchRow struct {
	Type   string `json:"type"`
	MetaID string `json:"metaId"`
	At     int64  `json:"at"`
}

// Item is one in-progress playback row.
type Item struct {
	Type        string  `json:"type"`
	MetaID      string  `json:"metaId"`
	VideoID     string  `json:"videoId,omitempty"`
	Season      *int    `json:"season,omitempty"`
	Episode     *int    `json:"episode,omitempty"`
	ProgressPct float64 `json:"progressPercent"`
	PositionMs  int64   `json:"positionMs,omitempty"`
	DurationMs  int64   `json:"durationMs,omitempty"`
	Played      bool    `json:"played"`
	At          int64   `json:"at"`
}

// Build assembles the watchlist and watched halves, dropping anything
// AIOStreams cannot use: non movie/series content, ids without an IMDb id,
// and series rows that carry no episode.
func Build(library []nuvio.LibraryItem, watched []nuvio.WatchedItem) (watchlist []WatchRow, watchedSet Watched, version string) {
	watchlist = make([]WatchRow, 0, len(library))
	for _, item := range library {
		typ, ok := metaType(item.ContentType)
		if !ok || !strings.HasPrefix(item.ContentID, "tt") {
			continue
		}
		watchlist = append(watchlist, WatchRow{
			Type:   typ,
			MetaID: item.ContentID,
			// Nuvio stores added_at in milliseconds; AIOStreams uses seconds.
			At: item.AddedAt / 1000,
		})
	}

	movieSet := map[string]struct{}{}
	episodeSet := map[string]struct{}{}
	for _, item := range watched {
		typ, ok := metaType(item.ContentType)
		if !ok || !strings.HasPrefix(item.ContentID, "tt") {
			continue
		}
		if typ == "movie" {
			movieSet[item.ContentID] = struct{}{}
			continue
		}
		// A series watched row without an episode cannot be a video id.
		if item.Season == nil || item.Episode == nil {
			continue
		}
		episodeSet[episodeID(item.ContentID, *item.Season, *item.Episode)] = struct{}{}
	}

	watchedSet.Movies = sortedKeys(movieSet)
	watchedSet.Episodes = sortedKeys(episodeSet)

	version = versionToken(watchedSet)
	return watchlist, watchedSet, version
}

// BuildItems turns Nuvio progress rows into pull items. Rows that are
// finished (position at or above duration, or the played threshold) are left
// out: they belong in `watched`, not in Continue Watching. Non movie/series
// rows and rows without an IMDb id are skipped.
func BuildItems(progress []nuvio.ProgressItem) []Item {
	items := make([]Item, 0, len(progress))
	for _, row := range progress {
		typ, ok := metaType(row.ContentType)
		if !ok || !strings.HasPrefix(row.ContentID, "tt") {
			continue
		}
		pct := percent(row.Position, row.Duration)
		if row.Duration > 0 && pct >= finishedPercent {
			continue
		}
		item := Item{
			Type:        typ,
			MetaID:      row.ContentID,
			ProgressPct: pct,
			PositionMs:  row.Position,
			DurationMs:  row.Duration,
			At:          row.LastWatched / 1000,
		}
		if typ == "series" {
			if row.Season == nil || row.Episode == nil {
				continue
			}
			item.Season = row.Season
			item.Episode = row.Episode
			item.VideoID = videoID(row.ContentID, *row.Season, *row.Episode)
		} else {
			item.VideoID = row.ContentID
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].At > items[j].At })
	return items
}

// VersionToken computes the version from the watched set. The version only
// describes what the watched gate depends on; it is stable across calls as long
// as the watched content is unchanged. `items` and `watchlist` are never gated
// by it.
func VersionToken(watched Watched) string {
	return versionToken(watched)
}

func percent(position, duration int64) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(position) / float64(duration) * 100
}

func videoID(showID string, season, episode int) string {
	return showID + ":" + itoa(season) + ":" + itoa(episode)
}

func versionToken(watched Watched) string {
	parts := make([]string, 0, len(watched.Movies)+len(watched.Episodes))
	for _, id := range watched.Movies {
		parts = append(parts, "m:"+id)
	}
	for _, id := range watched.Episodes {
		parts = append(parts, "e:"+id)
	}
	sort.Strings(parts)

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}

func metaType(contentType string) (string, bool) {
	switch contentType {
	case "movie":
		return "movie", true
	case "series":
		return "series", true
	default:
		return "", false
	}
}

func episodeID(showID string, season, episode int) string {
	return showID + ":" + itoa(season) + ":" + itoa(episode)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
