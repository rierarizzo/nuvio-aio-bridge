// Package config loads the bridge configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Defaults for the Nuvio cloud API and the metadata source.
const (
	DefaultNuvioAPIURL  = "https://api.nuvio.tv"
	DefaultNuvioAnonKey = "sb_publishable_1Clq8rlTVACkdcZuqr6_AD__xUUC_EN"
	DefaultMetadataURL  = "https://v3-cinemeta.strem.io"
	DefaultPort         = 8080
)

// Config is the bridge's runtime configuration. Credentials never appear in
// logs; the token only guards the addon routes.
type Config struct {
	NuvioEmail    string
	NuvioPassword string
	NuvioProfile  int
	NuvioAPIURL   string
	NuvioAnonKey  string
	MetadataURL   string
	BridgeToken   string
	Port          int
}

// Load reads and validates the configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		NuvioEmail:    os.Getenv("NUVIO_EMAIL"),
		NuvioPassword: os.Getenv("NUVIO_PASSWORD"),
		NuvioAPIURL:   envOr("NUVIO_API_URL", DefaultNuvioAPIURL),
		NuvioAnonKey:  envOr("NUVIO_ANON_KEY", DefaultNuvioAnonKey),
		MetadataURL:   envOr("METADATA_URL", DefaultMetadataURL),
		BridgeToken:   os.Getenv("BRIDGE_TOKEN"),
		Port:          DefaultPort,
	}

	if raw := os.Getenv("NUVIO_PROFILE"); raw != "" {
		profile, err := strconv.Atoi(raw)
		if err != nil || profile < 1 || profile > 6 {
			return Config{}, fmt.Errorf("NUVIO_PROFILE must be an integer from 1 to 6, got %q", raw)
		}
		cfg.NuvioProfile = profile
	}

	if raw := os.Getenv("PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("PORT must be a valid port number, got %q", raw)
		}
		cfg.Port = port
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports the first missing or invalid setting.
func (c Config) Validate() error {
	var problems []error
	if c.NuvioEmail == "" {
		problems = append(problems, errors.New("NUVIO_EMAIL is required"))
	}
	if c.NuvioPassword == "" {
		problems = append(problems, errors.New("NUVIO_PASSWORD is required"))
	}
	if c.NuvioProfile == 0 {
		problems = append(problems, errors.New("NUVIO_PROFILE is required"))
	}
	if c.BridgeToken == "" {
		problems = append(problems, errors.New("BRIDGE_TOKEN is required"))
	}
	return errors.Join(problems...)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
