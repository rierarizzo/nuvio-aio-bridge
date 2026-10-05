// Package manifest builds the Stremio/AIOStreams addon manifest.
package manifest

// Manifest is the addon manifest served at `/{token}/manifest.json`.
type Manifest struct {
	ID          string     `json:"id"`
	Version     string     `json:"version"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Types       []string   `json:"types"`
	Catalogs    []any      `json:"catalogs"`
	Resources   []Resource `json:"resources"`
	WatchState  WatchState `json:"watchState"`
}

// Resource is a declared addon resource.
type Resource struct {
	Name       string   `json:"name"`
	Types      []string `json:"types"`
	IDPrefixes []string `json:"idPrefixes"`
}

// WatchState describes the push and pull halves of the watch_state resource.
type WatchState struct {
	Version int  `json:"version"`
	Push    Push `json:"push"`
	Pull    Pull `json:"pull"`
}

// Push lists the events the bridge accepts from AIOStreams.
type Push struct {
	Events []string `json:"events"`
	// Bulk asks AIOStreams to send a whole-show or whole-season mark as one
	// event (with a `videos` list) instead of one event per video.
	Bulk bool `json:"bulk,omitempty"`
}

// Pull lists the parts of the answer the bridge fills and how long it is reused.
type Pull struct {
	Items      bool `json:"items"`
	Watched    bool `json:"watched"`
	Watchlist  bool `json:"watchlist"`
	TTLSeconds int  `json:"ttlSeconds"`
}

// Build returns the manifest for the current contract version.
func Build() Manifest {
	return Manifest{
		ID:          "local.nuvio-bridge",
		Version:     "0.1.0",
		Name:        "Nuvio Bridge",
		Description: "Sync with Nuvio",
		Types:       []string{"movie", "series"},
		Catalogs:    []any{},
		Resources: []Resource{
			{
				Name:       "watch_state",
				Types:      []string{"movie", "series"},
				IDPrefixes: []string{"tt"},
			},
		},
		WatchState: WatchState{
			Version: 2,
			Push: Push{
				Events: []string{"start", "pause", "stop", "played", "unplayed", "watchlisted", "unwatchlisted"},
				Bulk:   true,
			},
			Pull: Pull{
				Items:      true,
				Watched:    true,
				Watchlist:  true,
				TTLSeconds: 300,
			},
		},
	}
}
