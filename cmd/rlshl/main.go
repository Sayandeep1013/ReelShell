// Command rlshl is ReelShell's entry point: a terminal-native browser for
// movies, TV, and anime that hands playback off to mpv. See SPEC.md at the
// repo root for the full design.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/player"
)

type model struct {
	cfg      *config.Config
	mpvFound bool
}

func initialModel(cfg *config.Config) model {
	return model{cfg: cfg, mpvFound: player.CheckAvailable(cfg.MPV.Path)}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func (m model) View() string {
	status := okStyle.Render("mpv: found")
	if !m.mpvFound {
		status = warnStyle.Render("mpv: NOT on PATH — install it before playback will work")
	}

	return titleStyle.Render("ReelShell") + "\n\n" +
		"Data dir: " + m.cfg.General.DataDir + "\n" +
		status + "\n\n" +
		dimStyle.Render("Movies / TV / Anime browsing (TMDB + AniList) not wired up yet — scaffold only.") + "\n\n" +
		dimStyle.Render("press q to quit") + "\n"
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
