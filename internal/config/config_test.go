package config

import "testing"

// setRequired sets the four required variables to valid values. Individual
// tests clear the one under test afterwards.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("NUVIO_EMAIL", "a@b.c")
	t.Setenv("NUVIO_PASSWORD", "pw")
	t.Setenv("NUVIO_PROFILE", "1")
	t.Setenv("BRIDGE_TOKEN", "tok")
}

// clearOptional blanks every optional override so a value exported in the
// caller's shell cannot make a defaults test pass for the wrong reason.
func clearOptional(t *testing.T) {
	t.Helper()
	for _, key := range []string{"NUVIO_API_URL", "NUVIO_ANON_KEY", "METADATA_URL", "TMDB_API_KEY", "TMDB_BASE_URL", "PORT"} {
		t.Setenv(key, "")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	setRequired(t)
	clearOptional(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NuvioEmail != "a@b.c" || cfg.NuvioPassword != "pw" || cfg.NuvioProfile != 1 || cfg.BridgeToken != "tok" {
		t.Errorf("required values not applied: %+v", cfg)
	}
	if cfg.NuvioAPIURL != DefaultNuvioAPIURL {
		t.Errorf("NuvioAPIURL = %q, want %q", cfg.NuvioAPIURL, DefaultNuvioAPIURL)
	}
	if cfg.NuvioAnonKey != DefaultNuvioAnonKey {
		t.Errorf("NuvioAnonKey = %q, want default", cfg.NuvioAnonKey)
	}
	if cfg.MetadataURL != DefaultMetadataURL {
		t.Errorf("MetadataURL = %q, want %q", cfg.MetadataURL, DefaultMetadataURL)
	}
	if cfg.TMDBBaseURL != DefaultTMDBBaseURL {
		t.Errorf("TMDBBaseURL = %q, want %q", cfg.TMDBBaseURL, DefaultTMDBBaseURL)
	}
	if cfg.TMDBAPIKey != "" {
		t.Errorf("TMDBAPIKey = %q, want empty", cfg.TMDBAPIKey)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
	}
}

func TestLoadAppliesOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("NUVIO_API_URL", "https://example.test")
	t.Setenv("NUVIO_ANON_KEY", "key")
	t.Setenv("METADATA_URL", "https://meta.test")
	t.Setenv("TMDB_API_KEY", "tmdb-key")
	t.Setenv("TMDB_BASE_URL", "https://tmdb.test/3")
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NuvioAPIURL != "https://example.test" || cfg.NuvioAnonKey != "key" ||
		cfg.MetadataURL != "https://meta.test" || cfg.TMDBAPIKey != "tmdb-key" ||
		cfg.TMDBBaseURL != "https://tmdb.test/3" || cfg.Port != 9090 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadRejectsMissingRequired(t *testing.T) {
	for _, key := range []string{"NUVIO_EMAIL", "NUVIO_PASSWORD", "NUVIO_PROFILE", "BRIDGE_TOKEN"} {
		t.Run(key, func(t *testing.T) {
			setRequired(t)
			t.Setenv(key, "")
			if _, err := Load(); err == nil {
				t.Fatalf("missing %s should fail", key)
			}
		})
	}
}

func TestLoadRejectsBadProfile(t *testing.T) {
	// 0 lands on the "required" check rather than the range check, which is
	// the intended behaviour; the rest are out of range or unparsable.
	for _, raw := range []string{"0", "7", "-1", "abc", "1.5"} {
		t.Run(raw, func(t *testing.T) {
			setRequired(t)
			t.Setenv("NUVIO_PROFILE", raw)
			if _, err := Load(); err == nil {
				t.Fatalf("NUVIO_PROFILE=%q should fail", raw)
			}
		})
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	for _, raw := range []string{"0", "65536", "-1", "abc"} {
		t.Run(raw, func(t *testing.T) {
			setRequired(t)
			t.Setenv("PORT", raw)
			if _, err := Load(); err == nil {
				t.Fatalf("PORT=%q should fail", raw)
			}
		})
	}
}
