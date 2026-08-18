package discovery

// Kind identifies which catalogue a Content item came from — mirrors the
// provider protocol's "type" field (SPEC.md §5) exactly, so it can be
// passed straight through to a resolve request.
type Kind string

const (
	KindMovie Kind = "movie"
	KindTV    Kind = "tv"
	KindAnime Kind = "anime"
)

// Content is a single unified shape the TUI operates on, regardless of
// which underlying API (TMDB or AniList) it came from. Movie/TVShow/Anime
// stay as the real per-source API response shapes; Content is only ever
// constructed via the FromX adapters below.
type Content struct {
	Kind Kind
	ID   int

	Title    string
	Overview string
	Rating   float64 // always normalized to a 0-10 scale
	Year     string

	// Exactly one of these is set, matching Kind — see poster.FetchContent.
	TMDBPosterPath string
	AniListPosterURL string
}

func FromMovie(m Movie) Content {
	return Content{
		Kind: KindMovie, ID: m.ID, Title: m.Title, Overview: m.Overview,
		Rating: m.Rating, Year: m.Year, TMDBPosterPath: m.PosterPath,
	}
}

func FromTVShow(t TVShow) Content {
	return Content{
		Kind: KindTV, ID: t.ID, Title: t.Title, Overview: t.Overview,
		Rating: t.Rating, Year: t.Year, TMDBPosterPath: t.PosterPath,
	}
}

func FromAnime(a Anime) Content {
	return Content{
		Kind: KindAnime, ID: a.ID, Title: a.Title, Overview: a.Overview,
		Rating: a.Rating, Year: a.Year, AniListPosterURL: a.PosterURL,
	}
}
