// Package player launches mpv for playback (SPEC.md §2, §4, §6). ReelShell
// never renders video itself — it shells out to mpv, which opens its own
// window, and waits for it to exit.
package player

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/provider"
)

// minPlayDuration: if mpv exits before this much time has passed, the
// stream almost certainly never actually played (dead link, blocked,
// unsupported format) rather than the user just finishing a very short
// clip — so it's treated as a failure (IMPLEMENTATION_PLAN.md, v0 error
// table) instead of being silently marked "watched".
const minPlayDuration = 5 * time.Second

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
// against a resolved stream, passing through headers and any subtitle
// tracks the provider supplied. Multiple subtitles (different languages)
// each get their own --sub-file flag, so mpv's own `c` (cycle sub)
// keybind switches between them (SPEC.md v2: multi-language subtitles).
//
// ctx being cancelled (e.g. the user quits ReelShell while mpv is still
// playing) kills the mpv child process rather than leaving it orphaned —
// on Windows, a child process is not otherwise terminated just because its
// parent exited.
func Play(ctx context.Context, mpvPath string, res *provider.ResolveResponse) error {
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

	for _, sub := range res.Subtitles {
		if sub.URL != "" {
			args = append(args, "--sub-file="+sub.URL)
		}
	}

	cmd := exec.CommandContext(ctx, mpvPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Errorf("mpv exited with error: %w (stderr: %s)", err, tail(stderr.String(), 500))
	}
	if elapsed < minPlayDuration {
		return fmt.Errorf("mpv exited after only %s — the stream likely never played (stderr: %s)", elapsed.Round(time.Millisecond), tail(stderr.String(), 500))
	}
	return nil
}

func tail(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return "…" + s[len(s)-maxLen:]
}
