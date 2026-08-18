# ReelShell

A terminal-native tool to browse movies, series, and anime, and watch them for free — all from the terminal.

## Idea

We're making a terminal-native tool where you can browse movies, series, and anime and watch them for free. The engineering challenges involved are the real open question right now — that's the hard part, and it's genuinely uncertain whether they're solvable in a straightforward way. That's why there isn't much in this repo yet, and that's okay for now.

Reference points for the space: tools like Sonarr and Radarr (and similar indexers/library managers), plus some anime-related APKs that were tested separately (not part of this repo).

## Spec

Full design: [SPEC.md](./SPEC.md).

This repo is the public framework only — the TUI, discovery-layer clients (TMDB/AniList), and the provider protocol. Actual stream-source provider implementations live in a private companion repo, [ReelShell-Providers](https://github.com/Sayandeep1013/ReelShell-Providers), and are never published here.

## Status

**v0 complete** — real TMDB trending/search, movie detail screen, and full resolve→mpv playback verified end to end (see `cmd/rlshl/main_test.go`). Playback currently goes through a dummy provider that always returns a public-domain test clip; a real movie provider (private repo) is next, alongside v1 (TV/anime tabs, history, real provider protocol).
