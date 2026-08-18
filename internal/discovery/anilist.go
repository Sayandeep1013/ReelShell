package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const anilistURL = "https://graphql.anilist.co"

type Anime struct {
	ID          int
	Title       string
	Overview    string
	Rating      float64 // normalized to a 0-10 scale, matching Movie.Rating
	Year        string
	Episodes    int
	PosterURL   string
}

type AniListClient struct {
	http *http.Client
}

func NewAniListClient() *AniListClient {
	return &AniListClient{http: &http.Client{Timeout: 10 * time.Second}}
}

type anilistMediaTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
}

type anilistMedia struct {
	ID            int               `json:"id"`
	Title         anilistMediaTitle `json:"title"`
	AverageScore  int               `json:"averageScore"`
	Description   string            `json:"description"`
	CoverImage    struct {
		Medium string `json:"medium"`
	} `json:"coverImage"`
	StartDate struct {
		Year int `json:"year"`
	} `json:"startDate"`
	Episodes int `json:"episodes"`
}

type anilistPageResponse struct {
	Data struct {
		Page struct {
			Media []anilistMedia `json:"media"`
		} `json:"Page"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (a anilistMedia) toAnime() Anime {
	title := a.Title.English
	if title == "" {
		title = a.Title.Romaji
	}
	year := ""
	if a.StartDate.Year != 0 {
		year = fmt.Sprintf("%d", a.StartDate.Year)
	}
	return Anime{
		ID:        a.ID,
		Title:     title,
		Overview:  a.Description,
		Rating:    float64(a.AverageScore) / 10,
		Year:      year,
		Episodes:  a.Episodes,
		PosterURL: a.CoverImage.Medium,
	}
}

const animeFields = `
	id
	title { romaji english }
	averageScore
	description(asHtml: false)
	coverImage { medium }
	startDate { year }
	episodes
`

func (c *AniListClient) TrendingAnime() ([]Anime, error) {
	query := fmt.Sprintf(`query {
		Page(page: 1, perPage: 20) {
			media(type: ANIME, sort: TRENDING_DESC) { %s }
		}
	}`, animeFields)
	return c.run(query, nil)
}

func (c *AniListClient) SearchAnime(search string) ([]Anime, error) {
	if search == "" {
		return nil, nil
	}
	query := fmt.Sprintf(`query ($search: String) {
		Page(page: 1, perPage: 20) {
			media(type: ANIME, search: $search) { %s }
		}
	}`, animeFields)
	return c.run(query, map[string]any{"search": search})
}

func (c *AniListClient) run(query string, variables map[string]any) ([]Anime, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Post(anilistURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anilist: request failed (network/timeout): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("anilist: rate limited (429), try again shortly")
	}

	var parsed anilistPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("anilist: decoding response: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("anilist: %s", parsed.Errors[0].Message)
	}

	results := make([]Anime, len(parsed.Data.Page.Media))
	for i, m := range parsed.Data.Page.Media {
		results[i] = m.toAnime()
	}
	return results, nil
}
