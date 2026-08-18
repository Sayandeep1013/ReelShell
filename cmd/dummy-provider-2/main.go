// Command dummy-provider-2 is a second reference provider, identical in
// spirit to cmd/dummy-provider but returning a different public-domain
// test clip. Its only purpose is to give the "cycle preferred provider"
// feature (SPEC.md v2 provider toggle design) something real to cycle
// between — no source logic, safe to be public, same as dummy-provider.
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

	resp := resolveResponse{
		OK:  true,
		URL: "https://interactive-examples.mdn.mozilla.net/media/cc0-videos/flower.mp4",
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}
