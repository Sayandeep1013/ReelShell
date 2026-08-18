package main

import (
	"strings"
	"testing"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/discovery"
)

// TestHeaderMultiLineHelpAligned guards against the header() bug where
// helpFor()'s multi-line output (e.g. the detail screen's 4 lines) was
// concatenated as if it were a single line, dumping everything after the
// first help line at column 0 instead of staying right-aligned under it.
func TestHeaderMultiLineHelpAligned(t *testing.T) {
	m := model{
		cfg:      &config.Config{},
		width:    80,
		mpvFound: true,
		screen:   screenDetail,
		selected: discovery.FromMovie(discovery.Movie{Title: "Test"}),
	}

	got := m.header()
	lines := strings.Split(got, "\n")

	// helpFor() for screenDetail (no anime, single provider) returns 3
	// lines: "enter/p: play", "esc: back", "q: quit". Each of those lines
	// must appear indented well past column 0 (i.e. right-aligned, not
	// dumped flush-left) — that's the signature of the old bug.
	found := 0
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			continue
		}
		if strings.Contains(line, "esc: back") || strings.Contains(line, "q: quit") {
			indent := len(line) - len(trimmed)
			if indent < 10 {
				t.Fatalf("help line %q is not right-aligned (indent=%d): header=\n%s", line, indent, got)
			}
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected to find both 'esc: back' and 'q: quit' lines, found %d in:\n%s", found, got)
	}
}
