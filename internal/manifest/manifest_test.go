package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildGolden pins the serialized manifest. It is a contract with
// AIOStreams, so any change should be a deliberate one. Regenerate with:
//
//	UPDATE_GOLDEN=1 go test ./internal/manifest
func TestBuildGolden(t *testing.T) {
	got, err := json.MarshalIndent(Build(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "manifest.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 go test ./internal/manifest): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("manifest changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildContract asserts the fields AIOStreams relies on, so a breakage
// fails with a clear message instead of a golden diff.
func TestBuildContract(t *testing.T) {
	m := Build()

	if m.ID == "" || m.Version == "" || m.Name == "" {
		t.Errorf("identity fields must not be empty: %+v", m)
	}
	if len(m.Types) != 2 || m.Types[0] != "movie" || m.Types[1] != "series" {
		t.Errorf("types = %v", m.Types)
	}
	if len(m.Resources) != 1 {
		t.Fatalf("resources = %+v", m.Resources)
	}
	if m.Resources[0].Name != "watch_state" {
		t.Errorf("resource name = %q", m.Resources[0].Name)
	}
	if len(m.Resources[0].IDPrefixes) != 1 || m.Resources[0].IDPrefixes[0] != "tt" {
		t.Errorf("idPrefixes = %v", m.Resources[0].IDPrefixes)
	}
	if m.WatchState.Version != 2 {
		t.Errorf("watchState.version = %d, want 2", m.WatchState.Version)
	}
	if !m.WatchState.Push.Bulk {
		t.Error("push.bulk must be declared")
	}

	want := map[string]bool{
		"start": true, "pause": true, "stop": true, "played": true,
		"unplayed": true, "watchlisted": true, "unwatchlisted": true,
	}
	for _, event := range m.WatchState.Push.Events {
		delete(want, event)
	}
	if len(want) != 0 {
		t.Errorf("push events missing: %v", want)
	}

	p := m.WatchState.Pull
	if !p.Items || !p.Watched || !p.Watchlist || p.TTLSeconds != 300 {
		t.Errorf("pull = %+v", p)
	}
}
