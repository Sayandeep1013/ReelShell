// Package discovery implements the read-only catalogue/browsing layer
// (SPEC.md §2): TMDB for movies/TV. Nothing here ever resolves a stream —
// that's the provider layer's job.
package discovery

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const tmdbBaseURL = "https://api.themoviedb.org/3"

type Movie struct {
	ID       int     `json:"id"`
	Title    string  `json:"title"`
	Overview string  `json:"overview"`
	Rating   float64 `json:"vote_average"`
	Year     string  `json:"release_date"` // YYYY-MM-DD, truncated for display by callers
}

type tmdbMovieList struct {
	Results []Movie `json:"results"`
}

type tmdbErrorBody struct {
	StatusMessage string `json:"status_message"`
	StatusCode    int    `json:"status_code"`
}

type TMDBClient struct {
	apiKey string
	http   *http.Client
}

func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *TMDBClient) TrendingMovies() ([]Movie, error) {
	body, err := c.get("/trending/movie/day")
	if err != nil {
		return nil, err
	}
	var list tmdbMovieList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("tmdb: decoding trending movies: %w", err)
	}
	return list.Results, nil
}

func (c *TMDBClient) SearchMovies(query string) ([]Movie, error) {
	if query == "" {
		return nil, nil
	}
	body, err := c.get("/search/movie?query=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	var list tmdbMovieList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("tmdb: decoding search results: %w", err)
	}
	return list.Results, nil
}

func (c *TMDBClient) get(path string) ([]byte, error) {
	sep := "?"
	if len(path) > 0 && path[len(path)-1:] != "?" && containsQuery(path) {
		sep = "&"
	}
	fullURL := tmdbBaseURL + path + sep + "api_key=" + url.QueryEscape(c.apiKey)

	resp, err := c.http.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("tmdb: request failed (network/timeout): %w", err)
	}
	defer resp.Body.Close()

	body := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("tmdb: key invalid (401) — check config.toml's [tmdb] api_key")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("tmdb: rate limited (429), try again shortly")
	}
	if resp.StatusCode != http.StatusOK {
		var e tmdbErrorBody
		_ = json.Unmarshal(body, &e)
		if e.StatusMessage != "" {
			return nil, fmt.Errorf("tmdb: %s (status %d)", e.StatusMessage, resp.StatusCode)
		}
		return nil, fmt.Errorf("tmdb: unexpected status %d", resp.StatusCode)
	}

	return body, nil
}

func containsQuery(path string) bool {
	for _, c := range path {
		if c == '?' {
			return true
		}
	}
	return false
}
