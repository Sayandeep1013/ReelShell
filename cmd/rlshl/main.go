// Command rlshl is ReelShell's entry point: a terminal-native browser for
// movies, TV, and anime that hands playback off to mpv. See SPEC.md at the
// repo root for the full design.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/discovery"
	"github.com/Sayandeep1013/ReelShell/internal/player"
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

type model struct {
	cfg      *config.Config
	tmdb     *discovery.TMDBClient
	mpvFound bool
	loading  bool
	loadErr  error
	list     list.Model
}

func initialModel(cfg *config.Config) model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Trending Movies"
	l.SetShowStatusBar(false)

	return model{
		cfg:      cfg,
		tmdb:     discovery.NewTMDBClient(cfg.TMDB.APIKey),
		mpvFound: player.CheckAvailable(cfg.MPV.Path),
		loading:  true,
		list:     l,
	}
}

func fetchTrending(tmdb *discovery.TMDBClient) tea.Cmd {
	return func() tea.Msg {
		movies, err := tmdb.TrendingMovies()
		return trendingLoadedMsg{movies: movies, err: err}
	}
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
		items := make([]list.Item, len(msg.movies))
		for i, mv := range msg.movies {
			items[i] = movieItem{movie: mv}
		}
		m.list.SetItems(items)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
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

	if m.loading {
		return header + dimStyle.Render("loading trending movies…") + "\n"
	}
	if m.loadErr != nil {
		return header + errStyle.Render("failed to load trending movies: "+m.loadErr.Error()) + "\n"
	}

	return header + m.list.View() + "\n" + dimStyle.Render("press q to quit") + "\n"
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
