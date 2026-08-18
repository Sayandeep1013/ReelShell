// Command dummy-provider is a reference implementation of the ReelShell
// provider protocol (SPEC.md §5). It always resolves to a public-domain
// test clip, regardless of what's asked for — it exists to prove the
// TUI → provider → mpv pipeline end to end (IMPLEMENTATION_PLAN.md v0 task
// 7) before any real source-resolution logic exists. Safe to be public:
// there is no scraping or real source logic here at all.
package main

import (
	"encoding/json"
	"os"
)

type resolveResponse struct {
	OK  bool   `json:"ok"`
	URL string `json:"url"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "resolve" {
		os.Exit(2)
	}

	// Input is intentionally ignored — this provider always returns the
	// same known-good public-domain clip.
	resp := resolveResponse{
		OK:  true,
		URL: "https://www.w3schools.com/html/mov_bbb.mp4",
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}
