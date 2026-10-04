package push

import (
	"context"
	"testing"

	"github.com/keneth/nuvio-aio-bridge/internal/metadata"
	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

type fakeResolver struct {
	meta metadata.Meta
	ok   bool
}

func (f fakeResolver) Resolve(context.Context, string, string) (metadata.Meta, bool) {
	return f.meta, f.ok
}

type recorder struct {
	added    []nuvio.LibraryItem
	removed  []string
	marked   []nuvio.WatchedItem
	unmarked []nuvio.WatchedKey
	progress []nuvio.ProgressEntry
	delProg  []string
}

func (r *recorder) AddToLibrary(_ context.Context, i nuvio.LibraryItem) error {
	r.added = append(r.added, i)
	return nil
}
func (r *recorder) RemoveFromLibrary(_ context.Context, id string) error {
	r.removed = append(r.removed, id)
	return nil
}
func (r *recorder) MarkWatched(_ context.Context, i nuvio.WatchedItem) error {
	r.marked = append(r.marked, i)
	return nil
}
func (r *recorder) DeleteWatched(_ context.Context, k nuvio.WatchedKey) error {
	r.unmarked = append(r.unmarked, k)
	return nil
}
func (r *recorder) SetProgress(_ context.Context, e nuvio.ProgressEntry) error {
	r.progress = append(r.progress, e)
	return nil
}
func (r *recorder) DeleteProgress(_ context.Context, key string) error {
	r.delProg = append(r.delProg, key)
	return nil
}

func TestIMDbIDPrefersIDsMap(t *testing.T) {
	e := Event{MetaID: "kitsu:1", IDs: map[string]string{"imdb": "tt0903747"}}
	if got := e.IMDbID(); got != "tt0903747" {
		t.Errorf("IMDbID = %q", got)
	}
}

func TestIMDbIDFallsBackToMeta(t *testing.T) {
	e := Event{MetaID: "tt0903747"}
	if got := e.IMDbID(); got != "tt0903747" {
		t.Errorf("IMDbID = %q", got)
	}
}

func TestIMDbIDEmptyWhenNone(t *testing.T) {
	e := Event{MetaID: "kitsu:1"}
	if got := e.IMDbID(); got != "" {
		t.Errorf("IMDbID = %q, want empty", got)
	}
}

func TestProgressKey(t *testing.T) {
	movie := Event{Scope: "movie", MetaID: "tt0137523"}
	if got := movie.ProgressKey(); got != "tt0137523" {
		t.Errorf("movie key = %q", got)
	}
	episode := Event{Scope: "episode", MetaID: "tt0903747", Season: ptr(3), Episode: ptr(7)}
	if got := episode.ProgressKey(); got != "tt0903747_s3e7" {
		t.Errorf("episode key = %q", got)
	}
}

func TestApplyStopUnfinishedSetsProgress(t *testing.T) {
	rec := &recorder{}
	played := false
	e := Event{
		Event: "stop", Scope: "episode", MetaID: "tt0903747",
		Season: ptr(3), Episode: ptr(7), PositionMs: 100, DurationMs: 1000,
		Played: &played, At: 1700000000,
	}
	ok, err := Apply(context.Background(), rec, nil, e)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(rec.progress) != 1 {
		t.Fatalf("progress = %+v", rec.progress)
	}
	got := rec.progress[0]
	if got.VideoID != "tt0903747:3:7" || got.Position != 100 || got.LastWatched != 1700000000000 {
		t.Errorf("progress = %+v", got)
	}
}

func TestApplyMissingIMDbIsNoOp(t *testing.T) {
	rec := &recorder{}
	ok, err := Apply(context.Background(), rec, nil, Event{Event: "played", MetaID: "kitsu:1"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok should be false when there is no IMDb id")
	}
	if len(rec.marked) != 0 {
		t.Errorf("unexpected write: %+v", rec.marked)
	}
}

func TestApplyWatchlistedEnrichesMetadata(t *testing.T) {
	rec := &recorder{}
	resolver := fakeResolver{
		ok: true,
		meta: metadata.Meta{
			Name:        "American Horror Story",
			Poster:      "https://images.metahub.space/poster/tt1844624/img",
			Background:  "https://images.metahub.space/background/tt1844624/img",
			Description: "An anthology horror drama series.",
			Genres:      []string{"Drama", "Horror"},
			ReleaseInfo: "2011",
			IMDBRating:  8.0,
		},
	}
	e := Event{Event: "watchlisted", Scope: "series", MetaID: "tt1844624", At: 1700000000}

	ok, err := Apply(context.Background(), rec, resolver, e)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(rec.added) != 1 {
		t.Fatalf("added = %+v", rec.added)
	}
	got := rec.added[0]
	if got.Name != "American Horror Story" || got.Poster == "" || got.Background == "" {
		t.Errorf("metadata not enriched: %+v", got)
	}
	if got.IMDBRating == nil || *got.IMDBRating != 8.0 {
		t.Errorf("rating = %v", got.IMDBRating)
	}
}

func TestApplyWatchlistedWritesIDAloneWhenUnresolved(t *testing.T) {
	rec := &recorder{}
	e := Event{Event: "watchlisted", Scope: "movie", MetaID: "tt0137523", At: 1}
	if ok, err := Apply(context.Background(), rec, fakeResolver{ok: false}, e); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(rec.added) != 1 || rec.added[0].Name != "" {
		t.Errorf("added = %+v", rec.added)
	}
}

func TestApplyUnknownEventIsNoOp(t *testing.T) {
	rec := &recorder{}
	ok, err := Apply(context.Background(), rec, nil, Event{Event: "dropped", MetaID: "tt0137523"})
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no-op", ok, err)
	}
}

func TestApplyStartAndPauseSetProgress(t *testing.T) {
	for _, event := range []string{"start", "pause"} {
		rec := &recorder{}
		ok, err := Apply(context.Background(), rec, nil, Event{
			Event: event, Scope: "episode", MetaID: "tt0903747",
			Season: ptr(3), Episode: ptr(7), PositionMs: 100, DurationMs: 1000, At: 1,
		})
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", event, ok, err)
		}
		if len(rec.progress) != 1 || rec.progress[0].VideoID != "tt0903747:3:7" {
			t.Errorf("%s: progress = %+v", event, rec.progress)
		}
	}
}

func TestApplyPlayedClearsProgress(t *testing.T) {
	rec := &recorder{}
	e := Event{Event: "played", Scope: "episode", MetaID: "tt0903747",
		Season: ptr(3), Episode: ptr(7), At: 1}
	ok, err := Apply(context.Background(), rec, nil, e)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(rec.marked) != 1 {
		t.Fatalf("marked = %+v", rec.marked)
	}
	if len(rec.delProg) != 1 || rec.delProg[0] != "tt0903747_s3e7" {
		t.Fatalf("delProg = %v, want [tt0903747_s3e7]", rec.delProg)
	}
}

func TestApplyUnplayedClearsProgress(t *testing.T) {
	rec := &recorder{}
	e := Event{Event: "unplayed", Scope: "movie", MetaID: "tt0137523"}
	ok, err := Apply(context.Background(), rec, nil, e)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(rec.unmarked) != 1 {
		t.Fatalf("unmarked = %+v", rec.unmarked)
	}
	if len(rec.delProg) != 1 || rec.delProg[0] != "tt0137523" {
		t.Fatalf("delProg = %v, want [tt0137523]", rec.delProg)
	}
}

func TestApplyStopFinishedClearsProgress(t *testing.T) {
	rec := &recorder{}
	played := true
	e := Event{Event: "stop", Scope: "movie", MetaID: "tt0137523", Played: &played, At: 1}
	if ok, err := Apply(context.Background(), rec, nil, e); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(rec.marked) != 1 || len(rec.delProg) != 1 || rec.delProg[0] != "tt0137523" {
		t.Fatalf("marked=%+v delProg=%v", rec.marked, rec.delProg)
	}
}

func ptr(n int) *int { return &n }
