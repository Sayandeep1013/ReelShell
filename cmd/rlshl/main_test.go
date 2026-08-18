package main

import (
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
	cmd := resolveAndPlay(cfg, content, "")
	msg := cmd()

	result, ok := msg.(playFinishedMsg)
	if !ok {
		t.Fatalf("expected playFinishedMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("resolveAndPlay failed: %v", result.err)
	}
}
