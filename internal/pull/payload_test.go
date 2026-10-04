package pull

import (
	"testing"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

func TestBuildFiltersAndSorts(t *testing.T) {
	library := []nuvio.LibraryItem{
		{ContentID: "tt0903747", ContentType: "series", AddedAt: 20},
		{ContentID: "tt0137523", ContentType: "movie", AddedAt: 10},
		{ContentID: "streamed:match", ContentType: "sport", AddedAt: 30},
		{ContentID: "tmdb:550", ContentType: "movie", AddedAt: 40},
	}
	watched := []nuvio.WatchedItem{
		{ContentID: "tt0137523", ContentType: "movie"},
		{ContentID: "tt0903747", ContentType: "series", Season: ptr(3), Episode: ptr(7)},
		{ContentID: "tt0903747", ContentType: "series"}, // show-level, skipped
		{ContentID: "streamed:match", ContentType: "sport"},
	}

	watchlist, set, version := Build(library, watched)

	if len(watchlist) != 2 {
		t.Fatalf("watchlist = %+v", watchlist)
	}
	if len(set.Movies) != 1 || set.Movies[0] != "tt0137523" {
		t.Errorf("movies = %v", set.Movies)
	}
	if len(set.Episodes) != 1 || set.Episodes[0] != "tt0903747:3:7" {
		t.Errorf("episodes = %v", set.Episodes)
	}
	if version == "" {
		t.Error("version is empty")
	}
}

func TestVersionStableAndSensitive(t *testing.T) {
	_, _, v1 := Build(nil, nil)
	_, _, v2 := Build(nil, nil)
	if v1 != v2 {
		t.Errorf("version changed with identical input: %s vs %s", v1, v2)
	}

	watched := []nuvio.WatchedItem{{ContentID: "tt0137523", ContentType: "movie"}}
	_, _, v3 := Build(nil, watched)
	if v3 == v1 {
		t.Error("version should change when watched content changes")
	}
}

func TestVersionIgnoresWatchlist(t *testing.T) {
	lib := []nuvio.LibraryItem{{ContentID: "tt0137523", ContentType: "movie", AddedAt: 1}}
	_, _, withLib := Build(lib, nil)
	_, _, without := Build(nil, nil)
	if withLib != without {
		t.Errorf("version should ignore the watchlist: %s vs %s", withLib, without)
	}
}

func TestWatchlistAtIsSeconds(t *testing.T) {
	lib := []nuvio.LibraryItem{{ContentID: "tt0137523", ContentType: "movie", AddedAt: 1_700_000_000_000}}
	watchlist, _, _ := Build(lib, nil)
	if len(watchlist) != 1 || watchlist[0].At != 1_700_000_000 {
		t.Fatalf("watchlist = %+v", watchlist)
	}
}

func TestSeasonZeroKept(t *testing.T) {
	_, set, _ := Build(nil, []nuvio.WatchedItem{
		{ContentID: "tt0903747", ContentType: "series", Season: ptr(0), Episode: ptr(2)},
	})
	if len(set.Episodes) != 1 || set.Episodes[0] != "tt0903747:0:2" {
		t.Errorf("episodes = %v", set.Episodes)
	}
}

func TestBuildItemsFiltersFinishedAndSport(t *testing.T) {
	progress := []nuvio.ProgressItem{
		{ContentID: "tt0137523", ContentType: "movie", Position: 300, Duration: 1000, LastWatched: 5000},
		{ContentID: "tt0137524", ContentType: "movie", Position: 950, Duration: 1000, LastWatched: 6000}, // 95%, finished
		{ContentID: "tt0903747", ContentType: "series", Season: ptr(3), Episode: ptr(7), Position: 100, Duration: 1000, LastWatched: 4000},
		{ContentID: "streamed:x", ContentType: "sport", Position: 100, Duration: 1000},
		{ContentID: "tt0903748", ContentType: "series", Position: 100, Duration: 1000}, // no episode
	}
	items := BuildItems(progress)
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2", items)
	}
	if items[0].MetaID != "tt0137523" || items[0].VideoID != "tt0137523" {
		t.Errorf("movie item = %+v", items[0])
	}
	if items[0].ProgressPct != 30 {
		t.Errorf("pct = %v", items[0].ProgressPct)
	}
	if items[1].VideoID != "tt0903747:3:7" || items[1].Season == nil || *items[1].Season != 3 {
		t.Errorf("episode item = %+v", items[1])
	}
}

func TestBuildItemsKeepsUnknownDuration(t *testing.T) {
	items := BuildItems([]nuvio.ProgressItem{
		{ContentID: "tt0137523", ContentType: "movie", Position: 100, Duration: 0, LastWatched: 1},
	})
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1 (unknown duration is not finished)", items)
	}
}

func ptr(n int) *int { return &n }
