// Command rlshl is ReelShell's entry point: a terminal-native browser for
// movies, TV, and anime that hands playback off to mpv. See SPEC.md at the
// repo root for the full design.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/discovery"
	"github.com/Sayandeep1013/ReelShell/internal/player"
	"github.com/Sayandeep1013/ReelShell/internal/provider"
)

// searchDebounce is how long to wait after the last keystroke before firing
// a real TMDB search request (IMPLEMENTATION_PLAN.md, v0 error table:
// avoids hitting TMDB's rate limit on every keystroke).
const searchDebounce = 350 * time.Millisecond

// resolveTimeout bounds how long a provider gets to resolve a stream before
// it's treated as failed (IMPLEMENTATION_PLAN.md, v0 error table: a hung
// provider must not freeze the UI).
const resolveTimeout = 15 * time.Second

type screen int

const (
	screenBrowsing screen = iota
	screenDetail
	screenResolving
)

// movieItem adapts discovery.Movie to bubbles/list's list.Item interface.
type movieItem struct {
	movie discovery.Movie
}

func (i movieItem) Title() string {
	year := ""
	if len(i.movie.Year) >= 4 {
		year = " (" + i.movie.Year[:4] + ")"
	}
	return fmt.Sprintf("%s%s", i.movie.Title, year)
}

func (i movieItem) Description() string {
	return fmt.Sprintf("★ %.1f", i.movie.Rating)
}

func (i movieItem) FilterValue() string { return i.movie.Title }

type trendingLoadedMsg struct {
	movies []discovery.Movie
	err    error
}

type searchResultsMsg struct {
	query  string
	movies []discovery.Movie
	err    error
}

// debounceFireMsg carries a generation number; if it doesn't match the
// model's current generation, a newer keystroke has arrived since this
// timer was scheduled and the search is stale, so it's dropped.
type debounceFireMsg struct {
	gen   int
	query string
}

type playFinishedMsg struct {
	err error
}

type model struct {
	cfg      *config.Config
	tmdb     *discovery.TMDBClient
	mpvFound bool
	loading  bool
	loadErr  error
	list     list.Model

	searching   bool
	searchInput textinput.Model
	searchGen   int
	searchErr   error

	screen      screen
	selected    discovery.Movie
	resolveErr  error
	playErr     error
}

func initialModel(cfg *config.Config) model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Trending Movies"
	l.SetShowStatusBar(false)
	l.SetShowFilter(false) // we drive search ourselves against TMDB, not the built-in local filter

	ti := textinput.New()
	ti.Placeholder = "search movies…"
	ti.Prompt = "/ "

	return model{
		cfg:         cfg,
		tmdb:        discovery.NewTMDBClient(cfg.TMDB.APIKey),
		mpvFound:    player.CheckAvailable(cfg.MPV.Path),
		loading:     true,
		list:        l,
		searchInput: ti,
		screen:      screenBrowsing,
	}
}

func fetchTrending(tmdb *discovery.TMDBClient) tea.Cmd {
	return func() tea.Msg {
		movies, err := tmdb.TrendingMovies()
		return trendingLoadedMsg{movies: movies, err: err}
	}
}

func fetchSearch(tmdb *discovery.TMDBClient, query string) tea.Cmd {
	return func() tea.Msg {
		movies, err := tmdb.SearchMovies(query)
		return searchResultsMsg{query: query, movies: movies, err: err}
	}
}

func scheduleDebounce(gen int, query string) tea.Cmd {
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return debounceFireMsg{gen: gen, query: query}
	})
}

// resolveAndPlay runs the configured movie provider, then hands the result
// to mpv. Both steps happen inside one tea.Cmd so the TUI stays responsive
// while a real subprocess/mpv window runs in the background.
func resolveAndPlay(cfg *config.Config, movie discovery.Movie) tea.Cmd {
	return func() tea.Msg {
		if len(cfg.Providers.Movie) == 0 {
			return playFinishedMsg{err: fmt.Errorf("no movie provider configured")}
		}
		providerExe := cfg.Providers.Movie[0]

		req := provider.ResolveRequest{Type: "movie", Title: movie.Title, Year: yearInt(movie.Year)}
		res, err := provider.ResolveWithTimeout(providerExe, req, resolveTimeout)
		if err != nil {
			return playFinishedMsg{err: err}
		}
		if !res.OK {
			return playFinishedMsg{err: fmt.Errorf("provider: %s", res.Error)}
		}

		if err := player.Play(cfg.MPV.Path, res); err != nil {
			return playFinishedMsg{err: fmt.Errorf("mpv: %w", err)}
		}
		return playFinishedMsg{err: nil}
	}
}

func yearInt(dateStr string) int {
	var y int
	fmt.Sscanf(dateStr, "%4d", &y)
	return y
}

func setMovieItems(l *list.Model, movies []discovery.Movie) {
	items := make([]list.Item, len(movies))
	for i, mv := range movies {
		items[i] = movieItem{movie: mv}
	}
	l.SetItems(items)
}

func (m model) Init() tea.Cmd {
	return fetchTrending(m.tmdb)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-6)
		return m, nil

	case trendingLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		setMovieItems(&m.list, msg.movies)
		return m, nil

	case searchResultsMsg:
		if msg.query != m.searchInput.Value() {
			return m, nil // stale result from an earlier query, drop it
		}
		if msg.err != nil {
			m.searchErr = msg.err
			return m, nil
		}
		m.searchErr = nil
		m.list.Title = "Search: " + msg.query
		setMovieItems(&m.list, msg.movies)
		return m, nil

	case debounceFireMsg:
		if msg.gen != m.searchGen || msg.query == "" {
			return m, nil // superseded by a newer keystroke, or empty query
		}
		return m, fetchSearch(m.tmdb, msg.query)

	case playFinishedMsg:
		m.playErr = msg.err
		m.screen = screenDetail
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		switch msg.String() {
		case "esc":
			m.searching = false
			m.searchInput.Blur()
			m.searchInput.SetValue("")
			m.list.Title = "Trending Movies"
			m.searchErr = nil
			return m, fetchTrending(m.tmdb)
		case "ctrl+c":
			return m, tea.Quit
		}

		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.searchGen++
		return m, tea.Batch(cmd, scheduleDebounce(m.searchGen, m.searchInput.Value()))
	}

	switch m.screen {
	case screenDetail:
		switch msg.String() {
		case "esc", "backspace":
			m.screen = screenBrowsing
			m.playErr = nil
			return m, nil
		case "enter", "p":
			if !m.mpvFound {
				m.playErr = fmt.Errorf("mpv not on PATH, can't play")
				return m, nil
			}
			m.screen = screenResolving
			m.playErr = nil
			return m, resolveAndPlay(m.cfg, m.selected)
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil

	case screenResolving:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil // ignore input while a resolve/play is in flight

	default: // screenBrowsing
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.searching = true
			m.searchInput.Focus()
			return m, textinput.Blink
		case "enter":
			if item, ok := m.list.SelectedItem().(movieItem); ok {
				m.selected = item.movie
				m.screen = screenDetail
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func (m model) View() string {
	status := okStyle.Render("mpv: found")
	if !m.mpvFound {
		status = warnStyle.Render("mpv: NOT on PATH — install it before playback will work")
	}
	header := titleStyle.Render("ReelShell") + "  " + status + "\n\n"

	switch m.screen {
	case screenDetail:
		return header + m.detailView()
	case screenResolving:
		return header + "resolving \"" + m.selected.Title + "\"…\n\n" + dimStyle.Render("this may take a few seconds")
	}

	if m.searching {
		header += m.searchInput.View() + "\n\n"
	}
	if m.loading {
		return header + dimStyle.Render("loading trending movies…") + "\n"
	}
	if m.loadErr != nil {
		return header + errStyle.Render("failed to load trending movies: "+m.loadErr.Error()) + "\n"
	}
	if m.searchErr != nil {
		header += errStyle.Render("search failed: "+m.searchErr.Error()) + "\n\n"
	}

	footer := dimStyle.Render("enter: details • / search • q quit")
	if m.searching {
		footer = dimStyle.Render("esc to cancel search")
	}
	return header + m.list.View() + "\n" + footer + "\n"
}

func (m model) detailView() string {
	body := titleStyle.Render(m.selected.Title) + "\n"
	body += fmt.Sprintf("★ %.1f\n\n", m.selected.Rating)
	body += m.selected.Overview + "\n\n"
	if m.playErr != nil {
		body += errStyle.Render("play failed: "+m.playErr.Error()) + "\n\n"
	}
	body += dimStyle.Render("enter/p: play • esc: back • q: quit")
	return body
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config from", config.ConfigPath(), ":", err)
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(cfg))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
