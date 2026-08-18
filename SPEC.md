# ReelShell — Spec v1

A terminal-native, fast-opening tool to browse and watch movies, TV, and anime, for personal non-commercial use. Binary name: `rlshl`.

## 1. Non-goals / ground rules

- Personal, single-user use only. No redistribution.
- The **public** repo (`Sayandeep1013/ReelShell`) contains only the TUI, the discovery-layer clients, and the provider *protocol/interface*. It never contains a working stream-source implementation.
- Actual stream-source (provider) implementations live in a **private** companion repo (`Sayandeep1013/ReelShell-Providers`), cloned locally, never made public.

## 2. Architecture

```
┌─────────────────────────────────────────────┐
│  TUI Shell (Bubble Tea, Go)                  │
│  Movies / TV / Anime tabs, search, detail     │
└───────────────┬───────────────┬─────────────┘
                │               │
     ┌──────────▼─────┐   ┌─────▼──────────────┐
     │ Discovery layer │   │ Provider layer      │
     │ TMDB (movie/tv) │   │ (private, pluggable)│
     │ AniList/Jikan    │   │ resolves a stream    │
     │ (anime)          │   │ URL for a title       │
     └──────────────────┘   └─────────┬───────────┘
                                       │
                                 ┌─────▼─────┐
                                 │   mpv     │
                                 │ (own win.)│
                                 └───────────┘
```

- **Discovery layer**: read-only metadata — search, trending, posters, synopsis, season/episode structure. TMDB for movies/TV (free API key required), AniList (GraphQL, no key) or Jikan (REST wrapper over MyAnimeList, no key) for anime. Entirely legitimate, no gray area — this is all "browsing," no stream resolution happens here.
- **Provider layer**: given canonical title/episode info from the discovery layer, resolves an actual playable source. See §5 for protocol.
- **Playback**: `rlshl` shells out to `mpv` with the resolved URL. mpv opens its own lightweight window (not a TUI, not VLC-style chrome — just the video plus a minimal on-screen overlay, entirely keyboard-driven). When mpv exits, control returns to the TUI automatically.

## 3. Storage layout

Everything lives under `G:\ReelShell\` (kept off the NVMe on purpose, and kept in one place):

```
G:\ReelShell\
  config.toml       — app config (TMDB key, mpv path, provider list)
  history.db        — SQLite: watch history, continue-watching, favorites
  providers\        — private-repo provider executables, cloned/built here
  cache\torrents\   — ephemeral torrent-streaming buffer (safe to delete anytime)
  logs\             — rlshl + provider debug logs
```

No app state on `%APPDATA%` or the system drive — `rlshl` reads `G:\ReelShell\config.toml` on startup; if `G:` isn't mounted, it fails fast with a clear error telling the user to reconnect the drive.

## 4. UX flow

- Cold open (`rlshl`) → home screen, three tabs: **Movies / TV / Anime**. Each shows a Trending row and a Continue Watching row (poster-grid, from the discovery layer).
- `/` → search, live results as you type.
- Select a title → detail pane: synopsis, rating, season/episode list (TV/anime). For anime, a **sub/dub toggle** in this screen, defaulting to sub.
- Select a movie/episode → provider `resolve()` runs → on success, `mpv` launches; on failure, an inline error with a **retry** action (and, once multiple providers exist per type, falls through to the next one — v2).
- mpv exits → back to TUI; watch position/"watched" state saved to `history.db`.

**TUI keybindings:**

| Key | Action |
|---|---|
| `↑`/`↓` or `j`/`k` | Move selection |
| `←`/`→` or `h`/`l` | Switch tabs / navigate seasons |
| `Enter` | Select / drill in |
| `Esc` / `Backspace` | Back |
| `/` | Search |
| `s` (detail screen, anime) | Toggle sub/dub |
| `r` (on resolve failure) | Retry |
| `q` | Quit `rlshl` |

**mpv playback keybindings** — a small custom `input.conf`, shipped in the public repo and passed via `--input-conf`, overriding only these three keys (everything else — `space` pause, arrow-key seek, `f` fullscreen, `q` quit, volume — stays as mpv's stock defaults):

| Key | mpv command | Effect |
|---|---|---|
| `s` | `add speed -0.25` | Speed down |
| `d` | `add speed 0.25` | Speed up |
| `c` | `cycle sub` | Cycle subtitle track (incl. off) |

## 5. Provider protocol

A provider is a standalone executable (any language) living in `G:\ReelShell\providers\`. **Not** a Go native plugin — Go's `plugin` package doesn't support Windows, so providers are separate processes speaking JSON over stdin/stdout. `rlshl` invokes:

```
<provider-exe> resolve
```

**stdin** (JSON):
```json
{
  "type": "movie | tv | anime",
  "title": "canonical title from discovery layer",
  "year": 2020,
  "season": 1,
  "episode": 3,
  "sub_or_dub": "sub"
}
```

**stdout** (JSON), success:
```json
{
  "ok": true,
  "url": "https://... or http://127.0.0.1:PORT/stream",
  "headers": { "Referer": "...", "User-Agent": "..." },
  "subtitles": [
    { "lang": "en", "url": "https://..." },
    { "lang": "ja", "url": "https://..." }
  ]
}
```
`subtitles` is optional and can hold more than one track (v2: multi-language subtitles) — each gets its own `--sub-file` when handed to mpv, and the `c` keybind (§4) cycles between them.

**stdout** (JSON), failure:
```json
{ "ok": false, "error": "human-readable reason" }
```

`rlshl` passes `url`/`headers`/`subtitles` straight to `mpv` (`--http-header-fields`, one `--sub-file` per subtitle).

**Torrent-based providers** (e.g. a Nyaa provider) fit this same protocol without any core changes: internally they start a local sequential-download BitTorrent-streaming engine (e.g. Go's `anacrolix/torrent`) and return `http://127.0.0.1:PORT/stream` as `url`, exactly like an HTTP-source provider.

`config.toml` lists ordered provider executables per content type:

```toml
[general]
data_dir = "G:\\ReelShell"

[tmdb]
api_key = ""

[mpv]
path = ""          # empty = auto-detect on PATH

[providers]
movie = ["G:\\ReelShell\\providers\\movie-provider.exe", "G:\\ReelShell\\providers\\movie-provider-2.exe"]
tv    = ["G:\\ReelShell\\providers\\tv-provider.exe"]
anime = ["G:\\ReelShell\\providers\\anime-nyaa.exe"]
```

**Provider selection UX (v2).** Two mechanisms, layered:
- **Automatic fallback**: `resolveAndPlay` tries every configured provider for a Kind in order, stopping at the first success. Silent, no user interaction.
- **Preferred-provider toggle**: `n` on the detail screen cycles which provider is tried *first*, per content Kind, for the current session (not persisted to `config.toml`). The rest of the list still acts as fallback underneath. This was chosen over a dedicated provider-picker screen (more control, but another screen/more friction) and over cycle-only-on-failure (purely reactive, no way to pre-express a preference) — see `IMPLEMENTATION_PLAN.md` for the fuller trade-off writeup.

This was designed after real research into comparable OSS tools: `pystardust/ani-cli` (anime, 13.5k★) remains the actively-maintained reference for a single well-chosen source. On the movie/TV side, both dedicated CLI tools found (`movie-cli`, `mov-cli`) turned out to be unmaintained/archived — a signal that betting on any single source or tool in this space is fragile, which is exactly what the automatic multi-provider fallback (already built pre-toggle) is for.

## 6. Dependencies

- `mpv` — external, not bundled. On startup, `rlshl` checks PATH (or `config.toml`'s `mpv.path`); if missing, shows an install prompt instead of crashing.
- Go 1.26+ toolchain to build.

## 7. Phasing

- **v0** — Movies only. TMDB search/browse, one hand-wired provider (from the private repo), `mpv` launch. No history yet. Goal: prove the full pipeline end-to-end.
- **v1** — Add TV + Anime tabs (AniList/Jikan), sub/dub toggle, `history.db` (continue-watching), provider protocol formalized as above (not hand-wired), custom `input.conf`.
- **v2** — Fuzzy search, discovery-layer response caching, multiple providers per type with automatic fallback ordering, more provider examples (torrent-based included).

## 8. Explicitly out of scope (for now)

Downloading/offline storage as a first-class feature, account sync, mobile, bundling any real provider in the public repo.
