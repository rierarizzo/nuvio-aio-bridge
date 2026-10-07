package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/keneth/nuvio-aio-bridge/internal/nuvio"
)

type stubChecker struct {
	index int
	name  string
	err   error
}

func (s stubChecker) ProfileIndex(context.Context) (int, string, error) {
	return s.index, s.name, s.err
}

func TestValidateProfile(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := validateProfile(context.Background(), stubChecker{index: 1, name: "Main"}, log); err != nil {
		t.Errorf("valid profile: %v", err)
	}
	if err := validateProfile(context.Background(), stubChecker{err: nuvio.ErrProfileNotFound}, log); !errors.Is(err, nuvio.ErrProfileNotFound) {
		t.Errorf("missing profile should fail, got %v", err)
	}
	if err := validateProfile(context.Background(), stubChecker{err: nuvio.ErrAuth}, log); !errors.Is(err, nuvio.ErrAuth) {
		t.Errorf("rejected credentials should fail, got %v", err)
	}
	if err := validateProfile(context.Background(), stubChecker{err: errors.New("network")}, log); err != nil {
		t.Errorf("transient error should not fail startup, got %v", err)
	}
}

func TestProbeHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	u, err := url.Parse(ok.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", u.Port())
	if code := probeHealth(); code != 0 {
		t.Errorf("healthy probe = %d, want 0", code)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	ub, err := url.Parse(bad.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", ub.Port())
	if code := probeHealth(); code != 1 {
		t.Errorf("500 probe = %d, want 1", code)
	}

	// A port with nothing listening refuses the connection. Bind then close a
	// listener so the port is free without guessing one.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	uc, err := url.Parse(closed.URL)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	t.Setenv("PORT", uc.Port())
	if code := probeHealth(); code != 1 {
		t.Errorf("refused probe = %d, want 1", code)
	}
}
