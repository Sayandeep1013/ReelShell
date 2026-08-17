// Package player launches mpv for playback (SPEC.md §2, §4, §6). ReelShell
// never renders video itself — it shells out to mpv, which opens its own
// window, and waits for it to exit.
package player

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/provider"
)

//go:embed input.conf
var inputConf []byte

// CheckAvailable reports whether mpv can be found, either at the configured
// path or on PATH. Callers should show an install prompt rather than fail
// silently when this returns false.
func CheckAvailable(mpvPath string) bool {
	if mpvPath == "" {
		mpvPath = "mpv"
	}
	_, err := exec.LookPath(mpvPath)
	return err == nil
}

// Play writes the shared input.conf into DataDir (once) and launches mpv
// against a resolved stream, passing through headers and an external
// subtitle if the provider supplied them.
func Play(mpvPath string, res *provider.ResolveResponse) error {
	if mpvPath == "" {
		mpvPath = "mpv"
	}

	confPath := filepath.Join(config.DataDir, "input.conf")
	if err := os.WriteFile(confPath, inputConf, 0o644); err != nil {
		return err
	}

	args := []string{res.URL, "--input-conf=" + confPath}

	if len(res.Headers) > 0 {
		fields := make([]string, 0, len(res.Headers))
		for k, v := range res.Headers {
			fields = append(fields, k+": "+v)
		}
		args = append(args, "--http-header-fields="+strings.Join(fields, ","))
	}

	if res.SubtitleURL != "" {
		args = append(args, "--sub-file="+res.SubtitleURL)
	}

	cmd := exec.Command(mpvPath, args...)
	return cmd.Run()
}
