// Command rlshl is ReelShell's entry point: a terminal-native browser for
// movies, TV, and anime that hands playback off to mpv. See SPEC.md at the
// repo root for the full design.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/Sayandeep1013/ReelShell/internal/cache"
	"github.com/Sayandeep1013/ReelShell/internal/config"
	"github.com/Sayandeep1013/ReelShell/internal/discovery"
	"github.com/Sayandeep1013/ReelShell/internal/history"
	"github.com/Sayandeep1013/ReelShell/internal/player"
	"github.com/Sayandeep1013/ReelShell/internal/poster"
	"github.com/Sayandeep1013/ReelShell/internal/provider"
)

const searchDebounce = 350 * time.Millisecond
const resolveTimeout = 15 * time.Second
const posterMaxWidthPx = 180

// discoveryCacheTTL: how long a trending/search response stays cached
// before a repeat request hits TMDB/AniList again. 5 minutes is plenty for
// "trending" (which doesn't change that fast) and search-result reuse
// while switching tabs and back.
const discoveryCacheTTL = 5 * time.Minute

// continueWatchingCount: how many recently-watched items to prepend to a
// tab's trending list. Kept small and simple for v1 — these entries carry
// only title/id/kind (not a re-fetched poster/overview), a known
// limitation, not an oversight.
const continueWatchingCount = 3

const (
	headerRows = 5
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
	screenSeasonPicker
	screenEpisodePicker
	screenResolving
)

type contentItem struct{ c discovery.Content }

func (i contentItem) Title() string {
	prefix := ""
	if i.c.Continued {
		prefix = "↻ "
	}
	year := ""
	if len(i.c.Year) >= 4 {
		year = " (" + i.c.Year[:4] + ")"
	}
	return prefix + i.c.Title + year
}
func (i contentItem) Description() string {
	if i.c.Continued {
		return "continue watching"
	}
	return fmt.Sprintf("★ %.1f", i.c.Rating)
}
func (i contentItem) FilterValue() string { return i.c.Title }

type seasonItem struct{ s discovery.TVSeason }

func (i seasonItem) Title() string       { return fmt.Sprintf("Season %d", i.s.SeasonNumber) }
func (i seasonItem) Description() string { return fmt.Sprintf("%d episodes", i.s.EpisodeCount) }
func (i seasonItem) FilterValue() string { return i.Title() }

// episodeRef unifies TMDB's named TV episodes and AniList's numbered-only
// anime episodes into one pickable shape.
type episodeRef struct {
	number int
	label  string
}
type episodeItem struct{ e episodeRef }

func (i episodeItem) Title() string       { return i.e.label }
func (i episodeItem) Description() string { return "" }
func (i episodeItem) FilterValue() string { return i.e.label }

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

type seasonsLoadedMsg struct {
	contentID int
	seasons   []discovery.TVSeason
	err       error
}

type episodesLoadedMsg struct {
	contentID int
	episodes  []episodeRef
	err       error
}

type model struct {
	cfg      *config.Config
	tmdb     *discovery.TMDBClient
	anilist  *discovery.AniListClient
	history  *history.DB // nil if it failed to open — history features silently no-op
	dcache   *cache.Cache
	mpvFound bool

	tab         discovery.Kind
	loading     bool
	loadErr     error
	list        list.Model
	cachedItems []discovery.Content // source for instant local fuzzy filtering while typing
	width       int
	height      int

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

	// preferredProvider: which provider to try first per content Kind,
	// cycled with 'n' on the detail screen (v2 provider toggle design).
	// Session-only, not persisted to config.toml. The rest of the
	// configured providers for that Kind still act as automatic fallback
	// underneath, in their original order.
	preferredProvider map[discovery.Kind]int

	seasonList    list.Model
	seasonsErr    error
	seasonsLoad   bool
	selectedSeason discovery.TVSeason

	episodeList  list.Model
	episodesErr  error
	episodesLoad bool
}

func compactDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	d.ShowDescription = true
	return d
}

func initialModel(cfg *config.Config, hist *history.DB) model {
	l := list.New(nil, compactDelegate(), 0, 0)
	l.Title = "Trending"
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowHelp(false)

	seasons := list.New(nil, compactDelegate(), 0, 0)
	seasons.Title = "Seasons"
	seasons.SetShowStatusBar(false)
	seasons.SetShowFilter(false)
	seasons.SetShowHelp(false)

	episodes := list.New(nil, compactDelegate(), 0, 0)
	episodes.Title = "Episodes"
	episodes.SetShowStatusBar(false)
	episodes.SetShowFilter(false)
	episodes.SetShowHelp(false)

	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Prompt = "/ "

	return model{
		cfg:               cfg,
		tmdb:              discovery.NewTMDBClient(cfg.TMDB.APIKey),
		anilist:           discovery.NewAniListClient(),
		history:           hist,
		dcache:            cache.New(discoveryCacheTTL),
		mpvFound:          player.CheckAvailable(cfg.MPV.Path),
		tab:               discovery.KindMovie,
		loading:           true,
		list:              l,
		seasonList:        seasons,
		episodeList:       episodes,
		searchInput:       ti,
		screen:            screenBrowsing,
		preferredProvider: make(map[discovery.Kind]int),
	}
}

func (m model) recentlyWatched(tab discovery.Kind) []discovery.Content {
	if m.history == nil {
		return nil
	}
	entries, err := m.history.RecentlyWatched(string(tab), continueWatchingCount)
	if err != nil {
		return nil
	}
	items := make([]discovery.Content, len(entries))
	for i, e := range entries {
		items[i] = discovery.Content{Kind: tab, ID: e.ContentID, Title: e.Title, Continued: true}
	}
	return items
}

func (m model) fetchTrending(tab discovery.Kind) tea.Cmd {
	return func() tea.Msg {
		cacheKey := "trending:" + string(tab)
		if cached, ok := m.dcache.Get(cacheKey); ok {
			items := append(m.recentlyWatched(tab), cached.([]discovery.Content)...)
			return trendingLoadedMsg{tab: tab, items: items}
		}

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
		if err == nil {
			m.dcache.Set(cacheKey, items)
		}
		items = append(m.recentlyWatched(tab), items...)
		return trendingLoadedMsg{tab: tab, items: items, err: err}
	}
}

func (m model) fetchSearch(tab discovery.Kind, query string) tea.Cmd {
	return func() tea.Msg {
		cacheKey := "search:" + string(tab) + ":" + query
		if cached, ok := m.dcache.Get(cacheKey); ok {
			return searchResultsMsg{tab: tab, query: query, items: cached.([]discovery.Content)}
		}

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
		if err == nil {
			m.dcache.Set(cacheKey, items)
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

func (m model) fetchSeasons(c discovery.Content) tea.Cmd {
	return func() tea.Msg {
		seasons, err := m.tmdb.TVSeasons(c.ID)
		return seasonsLoadedMsg{contentID: c.ID, seasons: seasons, err: err}
	}
}

func (m model) fetchTVEpisodes(c discovery.Content, season int) tea.Cmd {
	return func() tea.Msg {
		eps, err := m.tmdb.TVEpisodes(c.ID, season)
		refs := make([]episodeRef, len(eps))
		for i, e := range eps {
			refs[i] = episodeRef{number: e.EpisodeNumber, label: fmt.Sprintf("%d. %s", e.EpisodeNumber, e.Name)}
		}
		return episodesLoadedMsg{contentID: c.ID, episodes: refs, err: err}
	}
}

// animeEpisodes builds a numbered episode list from the anime's known
// episode count — AniList doesn't reliably expose per-episode titles the
// way TMDB does for TV, so this is deliberately just "Episode N".
func animeEpisodes(c discovery.Content) []episodeRef {
	n := c.Episodes
	if n <= 0 {
		n = 1 // unknown count: still offer episode 1 rather than a dead end
	}
	refs := make([]episodeRef, n)
	for i := 0; i < n; i++ {
		refs[i] = episodeRef{number: i + 1, label: fmt.Sprintf("Episode %d", i+1)}
	}
	return refs
}

// resolveAndPlay tries providers for this content's Kind starting from
// preferredIdx (the user's manually-cycled preference, 'n' on the detail
// screen), wrapping around through the rest in their configured order,
// stopping at the first success (SPEC.md v2: multi-provider fallback +
// preferred-provider toggle). For anime, a dub request that fails is
// retried once as sub on that same provider before moving to the next
// (IMPLEMENTATION_PLAN.md v1: most sources don't have every title dubbed).
// On success, hands the result to mpv and records it to history.
func resolveAndPlay(cfg *config.Config, hist *history.DB, c discovery.Content, subOrDub string, season, episode, preferredIdx int) tea.Cmd {
	return func() tea.Msg {
		providers := orderedProviders(providersFor(cfg, c.Kind), preferredIdx)
		if len(providers) == 0 {
			return playFinishedMsg{err: fmt.Errorf("no %s provider configured", c.Kind)}
		}

		var lastErr error
		for _, providerExe := range providers {
			req := provider.ResolveRequest{Type: string(c.Kind), Title: c.Title, Year: yearInt(c.Year), Season: season, Episode: episode}
			if c.Kind == discovery.KindAnime {
				req.SubOrDub = subOrDub
			}

			res, err := provider.ResolveWithTimeout(providerExe, req, resolveTimeout)
			if err == nil && !res.OK && c.Kind == discovery.KindAnime && subOrDub == "dub" {
				req.SubOrDub = "sub"
				res, err = provider.ResolveWithTimeout(providerExe, req, resolveTimeout)
			}
			if err != nil {
				lastErr = err
				continue
			}
			if !res.OK {
				lastErr = fmt.Errorf("provider %s: %s", providerExe, res.Error)
				continue
			}

			if err := player.Play(cfg.MPV.Path, res); err != nil {
				lastErr = fmt.Errorf("mpv: %w", err)
				continue
			}

			if hist != nil {
				_ = hist.MarkWatched(string(c.Kind), c.ID, c.Title, season, episode)
			}
			return playFinishedMsg{err: nil}
		}

		return playFinishedMsg{err: fmt.Errorf("all providers failed, last error: %w", lastErr)}
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

// orderedProviders rotates providers so index idx comes first, preserving
// the relative order of the rest — the preferred one is tried first, and
// everything else still acts as fallback underneath, in its original order.
func orderedProviders(providers []string, idx int) []string {
	if len(providers) == 0 {
		return providers
	}
	idx = idx % len(providers)
	return append(append([]string{}, providers[idx:]...), providers[:idx]...)
}

// providerName returns a short display name for a provider's executable
// path — just the filename, not the full G:\ReelShell\providers\... path.
func providerName(path string) string {
	return filepath.Base(path)
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
	h := m.height - headerRows - appMargin
	m.list.SetSize(m.width-appMargin*2, h)
	m.seasonList.SetSize(m.width-appMargin*2, h)
	m.episodeList.SetSize(m.width-appMargin*2, h)
}

func (m model) openDetail(c discovery.Content) (model, tea.Cmd) {
	m.selected = c
	m.subOrDub = "sub"
	m.posterSixel = ""
	m.posterErr = nil
	m.screen = screenDetail
	return m, fetchPoster(c)
}

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
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.cachedItems = msg.items
		setContentItems(&m.list, msg.items)
		return m, nil

	case searchResultsMsg:
		if msg.tab != m.tab || msg.query != m.query {
			return m, nil
		}
		if msg.err != nil {
			m.searchErr = msg.err
			return m, nil
		}
		m.searchErr = nil
		m.list.Title = "Search results"
		m.cachedItems = msg.items
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

	case seasonsLoadedMsg:
		if msg.contentID != m.selected.ID {
			return m, nil
		}
		m.seasonsLoad = false
		if msg.err != nil {
			m.seasonsErr = msg.err
			return m, nil
		}
		items := make([]list.Item, len(msg.seasons))
		for i, s := range msg.seasons {
			items[i] = seasonItem{s: s}
		}
		m.seasonList.SetItems(items)
		return m, nil

	case episodesLoadedMsg:
		if msg.contentID != m.selected.ID {
			return m, nil
		}
		m.episodesLoad = false
		if msg.err != nil {
			m.episodesErr = msg.err
			return m, nil
		}
		items := make([]list.Item, len(msg.episodes))
		for i, e := range msg.episodes {
			items[i] = episodeItem{e: e}
		}
		m.episodeList.SetItems(items)
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
		return m.handleTypingKey(msg)
	}

	switch m.screen {
	case screenDetail:
		return m.handleDetailKey(msg)
	case screenSeasonPicker:
		return m.handleSeasonPickerKey(msg)
	case screenEpisodePicker:
		return m.handleEpisodePickerKey(msg)
	case screenResolving:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	default:
		return m.handleBrowsingKey(msg)
	}
}

func (m model) handleTypingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	// Instant local fuzzy match over whatever's already loaded, so typing
	// feels immediate; the debounced call below still fires and replaces
	// these with authoritative results once the real API responds.
	q := m.searchInput.Value()
	if q != "" && len(m.cachedItems) > 0 {
		setContentItems(&m.list, fuzzyFilter(m.cachedItems, q))
	}

	return m, tea.Batch(cmd, scheduleDebounce(m.searchGen, q))
}

// fuzzyFilter ranks cachedItems by fuzzy match against query, using
// sahilm/fuzzy (already a transitive dependency via bubbles/list).
func fuzzyFilter(items []discovery.Content, query string) []discovery.Content {
	titles := make([]string, len(items))
	for i, c := range items {
		titles[i] = c.Title
	}
	matches := fuzzy.Find(query, titles)
	result := make([]discovery.Content, len(matches))
	for i, match := range matches {
		result[i] = items[match.Index]
	}
	return result
}

func (m model) handleBrowsingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "right", "l", "tab":
		return m.switchTab(nextTab(m.tab))
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "n":
		providers := providersFor(m.cfg, m.selected.Kind)
		if len(providers) > 1 {
			m.preferredProvider[m.selected.Kind] = (m.preferredProvider[m.selected.Kind] + 1) % len(providers)
		}
		return m, nil
	case "enter", "p":
		if !m.mpvFound {
			m.playErr = fmt.Errorf("mpv not on PATH, can't play")
			return m, nil
		}
		m.playErr = nil
		switch m.selected.Kind {
		case discovery.KindMovie:
			m.screen = screenResolving
			return m, resolveAndPlay(m.cfg, m.history, m.selected, m.subOrDub, 0, 0, m.preferredProvider[m.selected.Kind])
		case discovery.KindTV:
			m.screen = screenSeasonPicker
			m.seasonsLoad = true
			m.seasonsErr = nil
			m.seasonList.SetItems(nil)
			return m, m.fetchSeasons(m.selected)
		default: // anime
			m.screen = screenEpisodePicker
			m.selectedSeason = discovery.TVSeason{}
			items := make([]list.Item, 0)
			for _, e := range animeEpisodes(m.selected) {
				items = append(items, episodeItem{e: e})
			}
			m.episodeList.SetItems(items)
			return m, nil
		}
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleSeasonPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.screen = screenDetail
		return m, nil
	case "enter":
		item, ok := m.seasonList.SelectedItem().(seasonItem)
		if !ok {
			return m, nil
		}
		m.selectedSeason = item.s
		m.screen = screenEpisodePicker
		m.episodesLoad = true
		m.episodesErr = nil
		m.episodeList.SetItems(nil)
		return m, m.fetchTVEpisodes(m.selected, item.s.SeasonNumber)
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.seasonList, cmd = m.seasonList.Update(msg)
	return m, cmd
}

func (m model) handleEpisodePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		if m.selected.Kind == discovery.KindTV {
			m.screen = screenSeasonPicker
		} else {
			m.screen = screenDetail
		}
		return m, nil
	case "enter":
		item, ok := m.episodeList.SelectedItem().(episodeItem)
		if !ok {
			return m, nil
		}
		m.screen = screenResolving
		return m, resolveAndPlay(m.cfg, m.history, m.selected, m.subOrDub, m.selectedSeason.SeasonNumber, item.e.number, m.preferredProvider[m.selected.Kind])
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.episodeList, cmd = m.episodeList.Update(msg)
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
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	okStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	errStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Align(lipgloss.Right)
	appStyle         = lipgloss.NewStyle().Padding(1, appMargin)
	posterColumn     = lipgloss.NewStyle().Width(posterMaxWidthPx/8 + 2)
	ratingStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
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
		if len(providersFor(m.cfg, m.selected.Kind)) > 1 {
			lines = append([]string{"n: next provider"}, lines...)
		}
		if m.selected.Kind == discovery.KindAnime {
			lines = append([]string{"s: sub/dub"}, lines...)
		}
	case m.screen == screenSeasonPicker || m.screen == screenEpisodePicker:
		lines = []string{"↑/↓ navigate", "enter: select", "esc: back", "q: quit"}
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
	} else {
		searchLine = dimStyle.Render(m.selected.Title)
	}

	return titleLine + "\n\n" + tabs + searchLine + "\n\n"
}

func (m model) View() string {
	var content string
	switch m.screen {
	case screenDetail:
		content = m.header() + m.detailView()
	case screenSeasonPicker:
		content = m.header() + m.pickerView(m.seasonsLoad, m.seasonsErr, m.seasonList)
	case screenEpisodePicker:
		content = m.header() + m.pickerView(m.episodesLoad, m.episodesErr, m.episodeList)
	case screenResolving:
		content = m.header() + "resolving \"" + m.selected.Title + "\"…\n\n" + dimStyle.Render("this may take a few seconds")
	default:
		content = m.header() + m.browsingView()
	}
	return appStyle.Render(content)
}

func (m model) pickerView(loading bool, err error, l list.Model) string {
	if loading {
		return dimStyle.Render("loading…")
	}
	if err != nil {
		return errStyle.Render("failed to load: " + err.Error())
	}
	return l.View()
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

	if providers := providersFor(m.cfg, m.selected.Kind); len(providers) > 1 {
		idx := m.preferredProvider[m.selected.Kind] % len(providers)
		infoBlock += "\n\n" + dimStyle.Render(fmt.Sprintf("provider: %s (%d/%d)", providerName(providers[idx]), idx+1, len(providers)))
	}

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

	hist, err := history.Open()
	if err != nil {
		// Non-fatal: history/continue-watching just won't work this run.
		fmt.Fprintln(os.Stderr, "warning: history.db unavailable:", err)
		hist = nil
	} else {
		defer hist.Close()
	}

	p := tea.NewProgram(initialModel(cfg, hist), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
