package metadata

import (
	"context"
	"testing"
)

type stubResolver struct {
	meta Meta
	ok   bool
}

func (s stubResolver) Resolve(context.Context, string, string) (Meta, bool) {
	return s.meta, s.ok
}

func TestChainPrefersFirstHit(t *testing.T) {
	first := stubResolver{meta: Meta{Name: "first"}, ok: true}
	second := stubResolver{meta: Meta{Name: "second"}, ok: true}
	meta, ok := NewChain(first, second).Resolve(context.Background(), "movie", "tt1")
	if !ok || meta.Name != "first" {
		t.Fatalf("meta=%+v ok=%v", meta, ok)
	}
}

func TestChainFallsBack(t *testing.T) {
	first := stubResolver{ok: false}
	second := stubResolver{meta: Meta{Name: "second"}, ok: true}
	meta, ok := NewChain(first, second).Resolve(context.Background(), "movie", "tt1")
	if !ok || meta.Name != "second" {
		t.Fatalf("meta=%+v ok=%v", meta, ok)
	}
}

func TestChainAllMiss(t *testing.T) {
	meta, ok := NewChain(stubResolver{}, nil).Resolve(context.Background(), "movie", "tt1")
	if ok || meta.Name != "" {
		t.Fatalf("meta=%+v ok=%v", meta, ok)
	}
}

func TestUpgradeMetahub(t *testing.T) {
	in := "https://images.metahub.space/poster/small/tt1844624/img"
	want := "https://images.metahub.space/poster/large/tt1844624/img"
	if got := upgradeMetahub(in, "poster"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := upgradeMetahub("", "poster"); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
}

func TestTMDBWithoutKeyMisses(t *testing.T) {
	client := NewTMDB("", "")
	if _, ok := client.Resolve(context.Background(), "movie", "tt1"); ok {
		t.Error("TMDB without a key should not resolve")
	}
}

func TestYear(t *testing.T) {
	if got := year("2011-10-05"); got != "2011" {
		t.Errorf("got %q", got)
	}
	if got := year(""); got != "" {
		t.Errorf("got %q", got)
	}
}
