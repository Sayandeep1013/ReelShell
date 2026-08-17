# ReelShell — Implementation Plan

Companion to `SPEC.md`. This tracks concrete build steps per milestone, plus every failure mode I can anticipate for each step and how it's countered — so problems are designed around up front instead of discovered mid-build.

**Ground rule for v0/v1 building:** we prove the pipeline with a legal public-domain test stream (e.g. Big Buck Bunny's public HTTP URL) wired through a dummy provider, *before* any real source-resolution logic is written. That work happens later, entirely in the private `ReelShell-Providers` repo, on your machine. Validating "does the architecture work" and "does a scraper work" are kept as separate concerns throughout.

---

## Cross-cutting risks (apply at every milestone)

| Risk | Counter |
|---|---|
| A panic in Bubble Tea's `Update`/`View` crashes the whole TUI with a raw stack trace | Every network/JSON/subprocess call returns an `error`, never panics; `main()` has a top-level `recover` that prints a friendly message instead of a stack trace |
| Network calls (TMDB/AniList/provider) hang with no timeout, freezing the UI | Every `http.Client` gets an explicit `Timeout`; provider subprocesses run via `exec.CommandContext` with a hard deadline (~15s), killed on expiry |
| Windows console (legacy `conhost`, not Windows Terminal) mangles Unicode titles (anime titles, accented names) | Document Windows Terminal as the supported terminal in the README; `go-runewidth` (already a dependency via Bubble Tea) handles width math correctly once the terminal itself renders UTF-8 |
| `G:` drive letter changes (re-enumerated after a reboot/USB re-plug) | `config.Load()` already fails fast; make the error message distinguish "G:\ doesn't exist at all" from "G:\ReelShell doesn't exist yet, creating it" so it's obvious what's wrong |
| Corporate/ISP DNS or firewall blocks TMDB/AniList domains | Discovery-layer errors are surfaced distinctly from provider-resolve errors, so it's clear whether the problem is "can't browse the catalogue" vs "can't play this specific title" |

---

## v0 — Prove the pipeline (movies only)

**Goal:** `rlshl` opens, shows a real TMDB movie catalogue, you pick one, a test video plays in mpv, control returns to the TUI.

### Tasks

1. Install mpv, confirm `player.CheckAvailable` returns true.
2. Get a free TMDB API key (themoviedb.org → account → API), put it in `config.toml`.
3. `internal/discovery`: TMDB client — `SearchMovies(query)`, `TrendingMovies()`, both returning title/year/rating/overview.
4. Home screen: Trending row rendered as a `bubbles/list` (text rows — title, year, rating; **not** actual bitmap posters, see decision note below).
5. `/` search wired to `SearchMovies`, debounced ~300ms so it's not firing a request per keystroke.
6. Detail screen for a selected movie (synopsis, rating).
7. A dummy provider executable (private repo) whose `resolve` always returns a public-domain test URL — validates the whole `TUI → provider protocol → mpv` chain without touching any real source.
8. Wire "play" → `provider.Resolve` → `player.Play`, mpv launches, control returns on exit, mark "watched" in memory (no persistence yet — that's v1).

**Decision — no real poster images in v0.** Actual bitmap image rendering inside a terminal requires Sixel or the Kitty graphics protocol, and Windows Terminal's support for either is inconsistent. Rather than build against a fragile feature, v0 (and v1) use a clean text list. Real inline posters becomes an explicit v2+ stretch goal if it's still wanted once the core loop works — not a silent scope cut, just sequenced after what actually blocks "does it work at all."

### Errors & counters

| Error | When | Counter |
|---|---|---|
| TMDB key missing/invalid → `401` | Any TMDB call | Validate the key on startup with one cheap call; if it fails, show a specific inline message ("TMDB key invalid — check config.toml") instead of generic network errors |
| TMDB rate limit → `429` | Fast typing in search | Debounce input; cache the last N query results in memory |
| `mpv` not on PATH | Startup | Already detected via `CheckAvailable`; **block the play action** with an inline message rather than letting `exec.Command` fail deep in the call stack |
| Provider subprocess hangs (slow/unresponsive) | Any resolve | `exec.CommandContext` with ~15s timeout, kill on expiry, surface "resolution timed out" with a retry key |
| Provider returns malformed JSON or exits non-zero | Any resolve | Already wrapped with `fmt.Errorf` context in `internal/provider`; surfaced as an inline TUI error, never a panic |
| mpv opens but the URL is dead/unreachable | Playback | Capture mpv's stderr; if mpv exits in under ~5s, treat as a failed play (don't mark "watched"), show mpv's stderr tail as the error |
| Terminal resize mid-session | Any screen | Handle `tea.WindowSizeMsg` and reflow instead of leaving a stale layout |

---

## v1 — TV + Anime, history, sub/dub, real provider protocol

**Goal:** three tabs, continue-watching persists across restarts, sub/dub choice works, providers are pluggable via config instead of hand-wired.

### Tasks

1. `internal/discovery`: AniList GraphQL client (search/trending anime + episode lists) — no API key needed, ~90 req/min unauthenticated.
2. `internal/discovery`: extend TMDB client for TV (`SearchTV`, season/episode listing).
3. Tabs UI: Movies / TV / Anime, switchable with `←`/`→` or `h`/`l` per spec.
4. `history.db` (SQLite) for continue-watching/favorites — **use `modernc.org/sqlite`, a pure-Go driver**, not `mattn/go-sqlite3`. The latter needs CGO plus a C compiler, which isn't set up on this machine and is a common source of broken Windows builds; the pure-Go driver avoids that entirely.
5. Sub/dub toggle on the detail screen (default sub), passed through as `sub_or_dub` in the resolve request.
6. **Dub fallback**: if a dub resolve fails (`ok:false`), automatically retry once with `sub` before showing an error — most sources don't have every title dubbed, and silently falling back is better UX than a dead end.
7. Formalize the provider protocol: replace the v0 dummy provider with the real JSON stdin/stdout contract from `SPEC.md` §5, still timeout-wrapped from v0.
8. Continue-watching position: launch mpv with `--input-ipc-server`, poll periodically while it runs.

### Errors & counters

| Error | When | Counter |
|---|---|---|
| AniList query malformed/rate-limited | Search/browse | Same cache+debounce pattern as TMDB |
| SQLite CGO build fails (no C compiler on this machine) | `go build` | Avoided by using `modernc.org/sqlite` from the start, not discovered as a build break later |
| Two `rlshl` instances write to `history.db` at once | Rare (personal tool, single user) | Enable WAL mode; not worth more engineering for a single-user app |
| History schema needs a new column later | Any future change | Minimal versioned migration: a `schema_version` row checked at startup, `ALTER TABLE` applied if behind |
| **mpv IPC on Windows uses named pipes, not Unix sockets** — most mpv IPC tutorials assume Linux/macOS `--input-ipc-server=/tmp/mpvsocket` | Continue-watching position polling | Use `--input-ipc-server=\\.\pipe\reelshell-mpv` explicitly, and a named-pipe-capable Go client (`Microsoft/go-winio`) — flagging this now because copying a Linux mpv-IPC tutorial verbatim will silently fail on Windows |
| Dub not available for a title | Sub/dub toggle | Auto-fallback to sub (task 6 above), not a dead-end error |

---

## v2 — Fuzzy search, caching, multi-provider fallback, torrent provider example

**Goal:** feels fast and resilient — typing filters instantly, a dead source doesn't block playback, Nyaa/torrent sources work through the same protocol.

### Tasks

1. Local fuzzy filtering (e.g. `sahilm/fuzzy`) over already-fetched trending/cached results for instant-feeling search; full remote search still hits TMDB/AniList for anything not cached.
2. In-memory LRU + TTL cache for discovery responses, cutting redundant API calls.
3. Multi-provider fallback: `config.toml`'s provider lists become real fallback chains — try each in order with the same per-provider timeout from v0, stop at first `ok:true`.
4. Example torrent-based provider (private repo) using Nyaa as a source: `anacrolix/torrent` (pure Go) for sequential-download streaming, exposing `http://127.0.0.1:PORT/stream` — fits the existing protocol with zero core changes, per `SPEC.md` §5.
5. Multi-language subtitles: providers can return more than one `subtitle_url`; load all via multiple `--sub-file` flags so mpv's `c` (cycle sub) works across languages, not just on/off.

### Errors & counters

| Error | When | Counter |
|---|---|---|
| Torrent provider finds no peers (strict NAT/firewall, no seeders) | Anime resolve via torrent provider | DHT + public trackers as fallback (built into `anacrolix/torrent`); hard timeout with a clear "no peers found" error rather than hanging indefinitely |
| Playback starts before enough of the torrent is buffered → mpv stalls | Torrent provider resolve | Provider waits for a minimum buffered window (first few seconds worth of data) before returning the local URL, not immediately on torrent add |
| First provider in a fallback chain is slow, delaying the whole chain | Multi-provider resolve | Keep the same ~15s per-provider timeout from v0; log which provider actually succeeded so the config list can be reordered to put the fastest first |
| Windows Firewall prompts on first run (torrent provider's local HTTP server binding a port) | First torrent-provider use | One-time click-through, not fatal — note it in the README so it isn't mistaken for an error |

---

## Build order

v0 tasks 1–8, in the order listed, each one runnable/testable before moving to the next. Same for v1 and v2. No milestone starts before the previous one's task list is actually working, not just written.
