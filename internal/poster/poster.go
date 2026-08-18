// Package poster renders a TMDB movie poster inline in the terminal via
// Sixel. This is best-effort and optional: SPEC.md deliberately keeps the
// core UI text-only because Sixel support is inconsistent across
// terminals, so any failure here (no network, non-Sixel terminal, decode
// error) is just returned as an error for the caller to skip, never a
// reason to break the rest of the UI.
package poster

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"net/http"
	"time"

	"github.com/mattn/go-sixel"
	xdraw "golang.org/x/image/draw"
)

const tmdbImageBase = "https://image.tmdb.org/t/p/w185"

// Fetch downloads and Sixel-encodes a poster, resized to roughly
// maxWidthPx wide, returning the raw escape-sequence bytes ready to be
// written straight into a terminal (or embedded in a Bubble Tea View()
// string).
func Fetch(posterPath string, maxWidthPx int) (string, error) {
	if posterPath == "" {
		return "", fmt.Errorf("no poster available for this title")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(tmdbImageBase + posterPath)
	if err != nil {
		return "", fmt.Errorf("fetching poster: %w", err)
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return "", fmt.Errorf("decoding poster: %w", err)
	}

	resized := resize(img, maxWidthPx)

	var buf bytes.Buffer
	enc := sixel.NewEncoder(&buf)
	if err := enc.Encode(resized); err != nil {
		return "", fmt.Errorf("sixel-encoding poster: %w", err)
	}
	return buf.String(), nil
}

func resize(src image.Image, maxWidthPx int) image.Image {
	b := src.Bounds()
	if b.Dx() <= maxWidthPx {
		return src
	}
	scale := float64(maxWidthPx) / float64(b.Dx())
	newW := maxWidthPx
	newH := int(float64(b.Dy()) * scale)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}
