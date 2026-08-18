package main

import (
	"context"
	"testing"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/discovery"
)

// TestResolveAndPlayDummyProvider exercises the exact resolveAndPlay code
// path the TUI uses on "play", end to end: real provider subprocess, real
// mpv launch, real playback of the public-domain test clip. Requires mpv
// and G:\ReelShell\providers\movie-provider.exe to exist locally — this is
// a personal-machine integration test, not meant to run in a clean CI env.
func TestResolveAndPlayDummyProvider(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{Path: ""},
		Providers: config.ProvidersConfig{
			Movie: []string{`G:\ReelShell\providers\movie-provider.exe`},
		},
	}

	content := discovery.FromMovie(discovery.Movie{Title: "Integration Test Movie", Year: "2020-01-01"})
	cmd := resolveAndPlay(context.Background(), cfg, nil, content, "", 0, 0, 0)
	msg := cmd()

	result, ok := msg.(playFinishedMsg)
	if !ok {
		t.Fatalf("expected playFinishedMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("resolveAndPlay failed: %v", result.err)
	}
}

func TestOrderedProvidersRotation(t *testing.T) {
	providers := []string{"a", "b", "c"}

	cases := []struct {
		idx  int
		want []string
	}{
		{0, []string{"a", "b", "c"}},
		{1, []string{"b", "c", "a"}},
		{2, []string{"c", "a", "b"}},
		{3, []string{"a", "b", "c"}}, // wraps via modulo
	}

	for _, tc := range cases {
		got := orderedProviders(providers, tc.idx)
		if len(got) != len(tc.want) {
			t.Fatalf("idx %d: expected %v, got %v", tc.idx, tc.want, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("idx %d: expected %v, got %v", tc.idx, tc.want, got)
			}
		}
	}
}
