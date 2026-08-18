// Package history persists watch history / continue-watching to a local
// SQLite database (SPEC.md §3, §5 — G:\ReelShell\history.db). Uses
// modernc.org/sqlite, a pure-Go driver, deliberately instead of
// mattn/go-sqlite3: the latter needs CGO plus a C compiler, which isn't
// set up on this machine and is a common source of broken Windows builds
// (IMPLEMENTATION_PLAN.md v1 error table).
package history

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Sayandeep1013/ReelShell/internal/config"
)

const schemaVersion = 1

type DB struct {
	sql *sql.DB
}

// Entry is one row of watch history: a specific (kind, contentID,
// season, episode) that was successfully played, with when.
type Entry struct {
	Kind      string
	ContentID int
	Title     string
	Season    int
	Episode   int
	WatchedAt time.Time
}

func Open() (*DB, error) {
	path := filepath.Join(config.DataDir, "history.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("history: opening %s: %w", path, err)
	}
	// A single-user, single-process personal tool — one connection avoids
	// SQLite's concurrent-writer locking entirely rather than needing WAL
	// mode tuning (IMPLEMENTATION_PLAN.md v1 error table).
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sql: sqlDB}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error { return db.sql.Close() }

func (db *DB) migrate() error {
	_, err := db.sql.Exec(`
		CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS watch_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			content_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			season INTEGER NOT NULL DEFAULT 0,
			episode INTEGER NOT NULL DEFAULT 0,
			watched_at INTEGER NOT NULL,
			UNIQUE(kind, content_id, season, episode)
		);
	`)
	if err != nil {
		return fmt.Errorf("history: creating schema: %w", err)
	}

	var count int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM schema_meta`).Scan(&count); err != nil {
		return fmt.Errorf("history: checking schema_meta: %w", err)
	}
	if count == 0 {
		if _, err := db.sql.Exec(`INSERT INTO schema_meta (version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("history: seeding schema_meta: %w", err)
		}
	}
	return nil
}

// MarkWatched records a successful play. Re-watching the same
// (kind, contentID, season, episode) just bumps its timestamp rather than
// creating a duplicate row.
func (db *DB) MarkWatched(kind string, contentID int, title string, season, episode int) error {
	_, err := db.sql.Exec(`
		INSERT INTO watch_history (kind, content_id, title, season, episode, watched_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, content_id, season, episode)
		DO UPDATE SET watched_at = excluded.watched_at, title = excluded.title
	`, kind, contentID, title, season, episode, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("history: marking watched: %w", err)
	}
	return nil
}

// RecentlyWatched returns up to limit most-recently-watched entries for a
// given kind, one per contentID (its single most recent episode/season),
// newest first.
func (db *DB) RecentlyWatched(kind string, limit int) ([]Entry, error) {
	rows, err := db.sql.Query(`
		SELECT kind, content_id, title, season, episode, MAX(watched_at) AS ts
		FROM watch_history
		WHERE kind = ?
		GROUP BY content_id
		ORDER BY ts DESC
		LIMIT ?
	`, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("history: querying recently watched: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var ts int64
		if err := rows.Scan(&e.Kind, &e.ContentID, &e.Title, &e.Season, &e.Episode, &ts); err != nil {
			return nil, fmt.Errorf("history: scanning row: %w", err)
		}
		e.WatchedAt = time.Unix(ts, 0)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
