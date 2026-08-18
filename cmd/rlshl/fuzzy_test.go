package main

import (
	"testing"

	"github.com/Sayandeep1013/ReelShell/internal/discovery"
)

func TestFuzzyFilterMatchesAndRanks(t *testing.T) {
	items := []discovery.Content{
		discovery.FromMovie(discovery.Movie{ID: 1, Title: "The Matrix"}),
		discovery.FromMovie(discovery.Movie{ID: 2, Title: "Spider-Man"}),
		discovery.FromMovie(discovery.Movie{ID: 3, Title: "Matrix Reloaded"}),
	}

	got := fuzzyFilter(items, "matrix")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches for 'matrix', got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Title != "The Matrix" && c.Title != "Matrix Reloaded" {
			t.Fatalf("unexpected match: %s", c.Title)
		}
	}
}

func TestFuzzyFilterNoMatch(t *testing.T) {
	items := []discovery.Content{
		discovery.FromMovie(discovery.Movie{ID: 1, Title: "The Matrix"}),
	}
	got := fuzzyFilter(items, "zzz_no_such_thing_zzz")
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %d", len(got))
	}
}
