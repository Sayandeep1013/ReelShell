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
	ID         int     `json:"id"`
	Title      string  `json:"title"`
	Overview   string  `json:"overview"`
	Rating     float64 `json:"vote_average"`
	Year       string  `json:"release_date"` // YYYY-MM-DD, truncated for display by callers
	PosterPath string  `json:"poster_path"`
}

type tmdbMovieList struct {
	Results []Movie `json:"results"`
}

// TVShow mirrors Movie but for TMDB's TV endpoints, which use different
// field names ("name"/"first_air_date" instead of "title"/"release_date").
type TVShow struct {
	ID         int     `json:"id"`
	Title      string  `json:"name"`
	Overview   string  `json:"overview"`
	Rating     float64 `json:"vote_average"`
	Year       string  `json:"first_air_date"`
	PosterPath string  `json:"poster_path"`
}

type tmdbTVList struct {
	Results []TVShow `json:"results"`
}

type TVSeason struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
}

type tvDetails struct {
	Seasons []TVSeason `json:"seasons"`
}

type TVEpisode struct {
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
}

type tvSeasonDetails struct {
	Episodes []TVEpisode `json:"episodes"`
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

func (c *TMDBClient) TrendingTV() ([]TVShow, error) {
	body, err := c.get("/trending/tv/day")
	if err != nil {
		return nil, err
	}
	var list tmdbTVList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("tmdb: decoding trending tv: %w", err)
	}
	return list.Results, nil
}

func (c *TMDBClient) SearchTV(query string) ([]TVShow, error) {
	if query == "" {
		return nil, nil
	}
	body, err := c.get("/search/tv?query=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	var list tmdbTVList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("tmdb: decoding tv search results: %w", err)
	}
	return list.Results, nil
}

// TVSeasons excludes season 0 (specials), which is usually noise for a
// "pick a season to watch" flow.
func (c *TMDBClient) TVSeasons(tvID int) ([]TVSeason, error) {
	body, err := c.get(fmt.Sprintf("/tv/%d", tvID))
	if err != nil {
		return nil, err
	}
	var details tvDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("tmdb: decoding tv seasons: %w", err)
	}
	var seasons []TVSeason
	for _, s := range details.Seasons {
		if s.SeasonNumber > 0 {
			seasons = append(seasons, s)
		}
	}
	return seasons, nil
}

func (c *TMDBClient) TVEpisodes(tvID, season int) ([]TVEpisode, error) {
	body, err := c.get(fmt.Sprintf("/tv/%d/season/%d", tvID, season))
	if err != nil {
		return nil, err
	}
	var details tvSeasonDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("tmdb: decoding tv episodes: %w", err)
	}
	return details.Episodes, nil
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
