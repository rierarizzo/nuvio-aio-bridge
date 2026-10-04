package nuvio

import "errors"

// ErrProfileNotFound is returned when the configured profile is absent.
var ErrProfileNotFound = errors.New("nuvio profile not found")

// Profile is one Nuvio profile. The stable identifier is Index.
type Profile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Index int    `json:"profile_index"`
}

// ProgressItem is one in-progress playback row.
type ProgressItem struct {
	ContentID   string `json:"content_id"`
	ContentType string `json:"content_type"`
	VideoID     string `json:"video_id"`
	Season      *int   `json:"season"`
	Episode     *int   `json:"episode"`
	Position    int64  `json:"position"`
	Duration    int64  `json:"duration"`
	LastWatched int64  `json:"last_watched"`
	ProgressKey string `json:"progress_key"`
}
