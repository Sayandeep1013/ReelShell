// Package provider implements the ReelShell provider protocol (SPEC.md §5):
// a provider is a standalone executable, invoked as `<exe> resolve`, that
// reads a ResolveRequest as JSON on stdin and writes a ResolveResponse as
// JSON on stdout. Real provider implementations live in the private
// ReelShell-Providers repo — this package only knows the wire format and
// how to invoke one.
package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

type ResolveRequest struct {
	Type     string `json:"type"` // "movie" | "tv" | "anime"
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	SubOrDub string `json:"sub_or_dub,omitempty"` // anime only: "sub" | "dub"
}

type ResolveResponse struct {
	OK          bool              `json:"ok"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	SubtitleURL string            `json:"subtitle_url,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// Resolve runs a single provider executable and returns its response.
// Callers wanting fallback across multiple providers (v2) loop over
// config.ProvidersConfig themselves and try the next one on failure.
func Resolve(providerExe string, req ResolveRequest) (*ResolveResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(providerExe, "resolve")
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("provider %s failed: %w (stderr: %s)", providerExe, err, stderr.String())
	}

	var resp ResolveResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("provider %s returned invalid JSON: %w", providerExe, err)
	}
	return &resp, nil
}
