// Package push parses AIOStreams watch_state events and maps them to Nuvio
// writes.
package push

import "encoding/json"

// Event is a watch_state push body. Fields are optional because each event
// type fills a different subset.
type Event struct {
	ID         string            `json:"id"`
	Event      string            `json:"event"`
	Scope      string            `json:"scope"`
	At         int64             `json:"at"`
	MetaID     string            `json:"metaId"`
	VideoID    string            `json:"videoId"`
	PositionMs int64             `json:"positionMs"`
	DurationMs int64             `json:"durationMs"`
	Played     *bool             `json:"played"`
	Season     *int              `json:"season"`
	Episode    *int              `json:"episode"`
	IDs        map[string]string `json:"ids"`

	// Bulk marks: `videos` is every video the show/season mark changed, and a
	// large mark is split into independent parts.
	Videos []Video `json:"videos"`
	Part   int     `json:"part"`
	Parts  int     `json:"parts"`
}

// Video is one episode inside a bulk mark.
type Video struct {
	VideoID string `json:"videoId"`
	Season  *int   `json:"season"`
	Episode *int   `json:"episode"`
}

// Parse decodes an event body.
func Parse(data []byte) (Event, error) {
	var event Event
	err := json.Unmarshal(data, &event)
	return event, err
}

// IMDbID returns the show/movie IMDb id, preferring the explicit ids map and
// falling back to the meta id when it is already an IMDb id.
func (e Event) IMDbID() string {
	if imdb := e.IDs["imdb"]; imdb != "" {
		return imdb
	}
	if isIMDb(e.MetaID) {
		return e.MetaID
	}
	return ""
}

// MetaType returns the AIOStreams media type from the scope.
func (e Event) MetaType() string {
	if e.Scope == "movie" {
		return "movie"
	}
	return "series"
}

// IsBulk reports a mark over a whole show or season. AIOStreams sends the
// `videos` list and a `series`/`season` scope only for bulk events, so a mark
// on one video stays on the single-event path even when its scope is `series`.
func (e Event) IsBulk() bool {
	return len(e.Videos) > 0 && (e.Scope == "series" || e.Scope == "season")
}

// ProgressKey mirrors Nuvio's progress_key: bare id for a movie,
// id_s<season>e<episode> for an episode.
func (e Event) ProgressKey() string {
	if e.Scope == "movie" || e.Season == nil || e.Episode == nil {
		return e.IMDbID()
	}
	return e.IMDbID() + "_s" + itoa(*e.Season) + "e" + itoa(*e.Episode)
}

func isIMDb(id string) bool {
	if len(id) < 3 || id[0] != 't' || id[1] != 't' {
		return false
	}
	for _, r := range id[2:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
