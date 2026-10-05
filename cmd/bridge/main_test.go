package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
