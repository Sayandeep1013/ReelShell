// Command rlshl is ReelShell's entry point: a terminal-native browser for
// movies, TV, and anime that hands playback off to mpv. See SPEC.md at the
// repo root for the full design.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/discovery"
	"github.com/Sayandeep1013/ReelShell/internal/player"
	"github.com/Sayandeep1013/ReelShell/internal/poster"
	"github.com/Sayandeep1013/ReelShell/internal/provider"
)

const searchDebounce = 350 * time.Millisecond
const resolveTimeout = 15 * time.Second
const posterMaxWidthPx = 180

const (
	headerRows = 5 // title/help, blank, tabs, search line, blank
	appMargin  = 2
)

var tabOrder = []discovery.Kind{discovery.KindMovie, discovery.KindTV, discovery.KindAnime}

func tabLabel(k discovery.Kind) string {
	switch k {
	case discovery.KindMovie:
		return "Movies"
	case discovery.KindTV:
		return "TV"
	default:
		return "Anime"
	}
}

type screen int

const (
	screenBrowsing screen = iota
	screenDetail
	screenResolving
)

type contentItem struct{ c discovery.Content }

func (i contentItem) Title() string {
	year := ""
	if len(i.c.Year) >= 4 {
		year = " (" + i.c.Year[:4] + ")"
	}
	return i.c.Title + year
}
func (i contentItem) Description() string { return fmt.Sprintf("★ %.1f", i.c.Rating) }
func (i contentItem) FilterValue() string { return i.c.Title }

type trendingLoadedMsg struct {
	tab   discovery.Kind
	items []discovery.Content
	err   error
}

type searchResultsMsg struct {
	tab   discovery.Kind
	query string
	items []discovery.Content
	err   error
}

type debounceFireMsg struct {
	gen   int
	query string
}

type playFinishedMsg struct{ err error }

type posterLoadedMsg struct {
	contentID int
	sixel     string
	err       error
}

type model struct {
	cfg      *config.Config
	tmdb     *discovery.TMDBClient
	anilist  *discovery.AniListClient
	mpvFound bool

	tab     discovery.Kind
	loading bool
	loadErr error
	list    list.Model
	width   int
	height  int

	typing      bool
	searchInput textinput.Model
	query       string
	searchGen   int
	searchErr   error

	screen      screen
	selected    discovery.Content
	subOrDub    string // "sub" | "dub", anime only
	posterSixel string
	posterErr   error
	playErr     error
}

func compactDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	d.ShowDescription = true
	return d
}

func initialModel(cfg *config.Config) model {
	l := list.New(nil, compactDelegate(), 0, 0)
	l.Title = "Trending"
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowHelp(false)

	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Prompt = "/ "

	return model{
		cfg:         cfg,
		tmdb:        discovery.NewTMDBClient(cfg.TMDB.APIKey),
		anilist:     discovery.NewAniListClient(),
		mpvFound:    player.CheckAvailable(cfg.MPV.Path),
		tab:         discovery.KindMovie,
		loading:     true,
		list:        l,
		searchInput: ti,
		screen:      screenBrowsing,
	}
}

func (m model) fetchTrending(tab discovery.Kind) tea.Cmd {
	return func() tea.Msg {
		var items []discovery.Content
		var err error
		switch tab {
		case discovery.KindMovie:
			var movies []discovery.Movie
			movies, err = m.tmdb.TrendingMovies()
			for _, mv := range movies {
				items = append(items, discovery.FromMovie(mv))
			}
		case discovery.KindTV:
			var shows []discovery.TVShow
			shows, err = m.tmdb.TrendingTV()
			for _, s := range shows {
				items = append(items, discovery.FromTVShow(s))
			}
		case discovery.KindAnime:
			var anime []discovery.Anime
			anime, err = m.anilist.TrendingAnime()
			for _, a := range anime {
				items = append(items, discovery.FromAnime(a))
			}
		}
		return trendingLoadedMsg{tab: tab, items: items, err: err}
	}
}

func (m model) fetchSearch(tab discovery.Kind, query string) tea.Cmd {
	return func() tea.Msg {
		var items []discovery.Content
		var err error
		switch tab {
		case discovery.KindMovie:
			var movies []discovery.Movie
			movies, err = m.tmdb.SearchMovies(query)
			for _, mv := range movies {
				items = append(items, discovery.FromMovie(mv))
			}
		case discovery.KindTV:
			var shows []discovery.TVShow
			shows, err = m.tmdb.SearchTV(query)
			for _, s := range shows {
				items = append(items, discovery.FromTVShow(s))
			}
		case discovery.KindAnime:
			var anime []discovery.Anime
			anime, err = m.anilist.SearchAnime(query)
			for _, a := range anime {
				items = append(items, discovery.FromAnime(a))
			}
		}
		return searchResultsMsg{tab: tab, query: query, items: items, err: err}
	}
}

func scheduleDebounce(gen int, query string) tea.Cmd {
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return debounceFireMsg{gen: gen, query: query}
	})
}

func fetchPoster(c discovery.Content) tea.Cmd {
	return func() tea.Msg {
		sixel, err := poster.FetchContent(c, posterMaxWidthPx)
		return posterLoadedMsg{contentID: c.ID, sixel: sixel, err: err}
	}
}

// resolveAndPlay runs the configured provider for this content's Kind, then
// hands the result to mpv. For anime, a dub request that fails automatically
// retries once as sub before surfacing an error (IMPLEMENTATION_PLAN.md v1:
// most sources don't have every title dubbed, so silently falling back is
// better UX than a dead end).
func resolveAndPlay(cfg *config.Config, c discovery.Content, subOrDub string) tea.Cmd {
	return func() tea.Msg {
		providers := providersFor(cfg, c.Kind)
		if len(providers) == 0 {
			return playFinishedMsg{err: fmt.Errorf("no %s provider configured", c.Kind)}
		}
		providerExe := providers[0]

		req := provider.ResolveRequest{Type: string(c.Kind), Title: c.Title, Year: yearInt(c.Year)}
		if c.Kind == discovery.KindAnime {
			req.SubOrDub = subOrDub
		}

		res, err := provider.ResolveWithTimeout(providerExe, req, resolveTimeout)
		if err == nil && !res.OK && c.Kind == discovery.KindAnime && subOrDub == "dub" {
			req.SubOrDub = "sub"
			res, err = provider.ResolveWithTimeout(providerExe, req, resolveTimeout)
		}
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

func providersFor(cfg *config.Config, k discovery.Kind) []string {
	switch k {
	case discovery.KindMovie:
		return cfg.Providers.Movie
	case discovery.KindTV:
		return cfg.Providers.TV
	default:
		return cfg.Providers.Anime
	}
}

func yearInt(dateStr string) int {
	var y int
	fmt.Sscanf(dateStr, "%4d", &y)
	return y
}

func setContentItems(l *list.Model, items []discovery.Content) {
	listItems := make([]list.Item, len(items))
	for i, c := range items {
		listItems[i] = contentItem{c: c}
	}
	l.SetItems(listItems)
}

func (m model) selectedContent() (discovery.Content, bool) {
	item, ok := m.list.SelectedItem().(contentItem)
	if !ok {
		return discovery.Content{}, false
	}
	return item.c, true
}

func (m *model) applyListLayout() {
	m.list.SetSize(m.width-appMargin*2, m.height-headerRows-appMargin)
}

func (m model) openDetail(c discovery.Content) (model, tea.Cmd) {
	m.selected = c
	m.subOrDub = "sub"
	m.posterSixel = ""
	m.posterErr = nil
	m.screen = screenDetail
	return m, fetchPoster(c)
}

// switchTab clears any active search and reloads trending for the new tab.
func (m model) switchTab(tab discovery.Kind) (model, tea.Cmd) {
	m.tab = tab
	m.typing = false
	m.query = ""
	m.searchInput.SetValue("")
	m.searchErr = nil
	m.loading = true
	m.loadErr = nil
	m.list.Title = "Trending " + tabLabel(tab)
	return m, m.fetchTrending(tab)
}

func (m model) Init() tea.Cmd {
	return m.fetchTrending(m.tab)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyListLayout()
		return m, nil

	case trendingLoadedMsg:
		if msg.tab != m.tab {
			return m, nil // arrived after the user switched tabs again
		}
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		setContentItems(&m.list, msg.items)
		return m, nil

	case searchResultsMsg:
		if msg.tab != m.tab || msg.query != m.query {
			return m, nil // stale: tab or query changed since this was fired
		}
		if msg.err != nil {
			m.searchErr = msg.err
			return m, nil
		}
		m.searchErr = nil
		m.list.Title = "Search results"
		setContentItems(&m.list, msg.items)
		return m, nil

	case debounceFireMsg:
		if msg.gen != m.searchGen || msg.query == "" {
			return m, nil
		}
		m.query = msg.query
		return m, m.fetchSearch(m.tab, msg.query)

	case posterLoadedMsg:
		if msg.contentID != m.selected.ID {
			return m, nil
		}
		m.posterSixel = msg.sixel
		m.posterErr = msg.err
		return m, nil

	case playFinishedMsg:
		m.playErr = msg.err
		m.screen = screenDetail
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.typing {
		switch msg.String() {
		case "esc":
			m.typing = false
			m.query = ""
			m.searchInput.SetValue("")
			m.searchInput.Blur()
			m.searchErr = nil
			m.list.Title = "Trending " + tabLabel(m.tab)
			return m, m.fetchTrending(m.tab)
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "down":
			m.typing = false
			m.searchInput.Blur()
			q := m.searchInput.Value()
			if q == "" {
				return m, nil
			}
			m.searchGen++
			if q == m.query {
				return m, nil
			}
			m.query = q
			return m, m.fetchSearch(m.tab, q)
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
		case "s":
			if m.selected.Kind == discovery.KindAnime {
				if m.subOrDub == "sub" {
					m.subOrDub = "dub"
				} else {
					m.subOrDub = "sub"
				}
			}
			return m, nil
		case "enter", "p":
			if !m.mpvFound {
				m.playErr = fmt.Errorf("mpv not on PATH, can't play")
				return m, nil
			}
			m.screen = screenResolving
			m.playErr = nil
			return m, resolveAndPlay(m.cfg, m.selected, m.subOrDub)
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil

	case screenResolving:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	default: // screenBrowsing
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.typing = true
			m.searchInput.Focus()
			return m, textinput.Blink
		case "esc":
			if m.query != "" {
				m.query = ""
				m.searchInput.SetValue("")
				m.list.Title = "Trending " + tabLabel(m.tab)
				m.searchErr = nil
				return m, m.fetchTrending(m.tab)
			}
			return m, nil
		case "enter":
			if c, ok := m.selectedContent(); ok {
				return m.openDetail(c)
			}
			return m, nil
		case "left", "h":
			return m.switchTab(prevTab(m.tab))
		case "right", "l":
			return m.switchTab(nextTab(m.tab))
		case "tab":
			return m.switchTab(nextTab(m.tab))
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func prevTab(t discovery.Kind) discovery.Kind {
	for i, k := range tabOrder {
		if k == t {
			return tabOrder[(i-1+len(tabOrder))%len(tabOrder)]
		}
	}
	return t
}

func nextTab(t discovery.Kind) discovery.Kind {
	for i, k := range tabOrder {
		if k == t {
			return tabOrder[(i+1)%len(tabOrder)]
		}
	}
	return t
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Align(lipgloss.Right)
	appStyle      = lipgloss.NewStyle().Padding(1, appMargin)
	posterColumn  = lipgloss.NewStyle().Width(posterMaxWidthPx/8 + 2)
	ratingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Underline(true)
	inactiveTabStyle = dimStyle
)

func (m model) helpFor() string {
	var lines []string
	switch {
	case m.typing:
		lines = []string{"enter/↓: browse results", "esc: cancel"}
	case m.screen == screenDetail:
		lines = []string{"enter/p: play", "esc: back", "q: quit"}
		if m.selected.Kind == discovery.KindAnime {
			lines = append([]string{"s: sub/dub"}, lines...)
		}
	case m.screen == screenResolving:
		lines = []string{"please wait…"}
	default:
		lines = []string{"←/→ switch tab", "↑/↓ navigate", "enter: details", "/ search", "q: quit"}
	}
	return helpStyle.Render(strings.Join(lines, "\n"))
}

func (m model) tabsLine() string {
	parts := make([]string, len(tabOrder))
	for i, k := range tabOrder {
		style := inactiveTabStyle
		if k == m.tab {
			style = activeTabStyle
		}
		parts[i] = style.Render(tabLabel(k))
	}
	return strings.Join(parts, "   ")
}

func (m model) header() string {
	status := okStyle.Render("mpv: found")
	if !m.mpvFound {
		status = warnStyle.Render("mpv: NOT on PATH")
	}
	left := titleStyle.Render("ReelShell") + "  " + status
	help := m.helpFor()

	titleLine := left
	if m.width > 0 {
		gap := m.width - appMargin*2 - lipgloss.Width(left) - lipgloss.Width(help)
		if gap < 1 {
			gap = 1
		}
		titleLine = left + strings.Repeat(" ", gap) + help
	}

	tabs := ""
	searchLine := dimStyle.Render("press / to search")
	if m.screen == screenBrowsing {
		tabs = m.tabsLine() + "\n\n"
		if m.typing {
			searchLine = m.searchInput.View()
		} else if m.query != "" {
			searchLine = dimStyle.Render("/ " + m.query + "  (esc to clear)")
		}
		searchLine += "\n"
	}

	return titleLine + "\n\n" + tabs + searchLine + "\n"
}

func (m model) View() string {
	var content string
	switch m.screen {
	case screenDetail:
		content = m.header() + m.detailView()
	case screenResolving:
		content = m.header() + "resolving \"" + m.selected.Title + "\"…\n\n" + dimStyle.Render("this may take a few seconds")
	default:
		content = m.header() + m.browsingView()
	}
	return appStyle.Render(content)
}

func (m model) browsingView() string {
	if m.loading {
		return dimStyle.Render("loading…")
	}
	if m.loadErr != nil {
		return errStyle.Render("failed to load: " + m.loadErr.Error())
	}
	if m.searchErr != nil {
		return errStyle.Render("search failed: "+m.searchErr.Error()) + "\n\n" + m.list.View()
	}
	return m.list.View()
}

func (m model) detailView() string {
	posterBlock := dimStyle.Render("loading poster…")
	if m.posterSixel != "" {
		posterBlock = m.posterSixel
	} else if m.posterErr != nil {
		posterBlock = dimStyle.Render("(no poster available)")
	}

	infoWidth := m.width - appMargin*2 - lipgloss.Width(posterColumn.Render(""))
	if infoWidth < 20 {
		infoWidth = 20
	}
	info := lipgloss.NewStyle().Width(infoWidth)

	infoBlock := titleStyle.Render(m.selected.Title) + "\n" +
		ratingStyle.Render(fmt.Sprintf("★ %.1f", m.selected.Rating))
	if m.selected.Kind == discovery.KindAnime {
		infoBlock += dimStyle.Render("  [" + strings.ToUpper(m.subOrDub) + "]")
	}
	infoBlock += "\n\n" + info.Render(m.selected.Overview)

	if m.playErr != nil {
		infoBlock += "\n\n" + errStyle.Render("play failed: "+m.playErr.Error())
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, posterColumn.Render(posterBlock), infoBlock)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config from", config.ConfigPath(), ":", err)
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
