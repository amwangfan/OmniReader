package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Migration struct {
	Version int
	Name    string
	SQL     string
}

var migrations = []Migration{
	{
		Version: 1,
		Name:    "core_schema",
		SQL: `
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash TEXT NOT NULL,
  client_label TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS books (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  author TEXT NOT NULL DEFAULT '',
  format TEXT NOT NULL,
  storage_key TEXT NOT NULL UNIQUE,
  file_size INTEGER NOT NULL,
  checksum TEXT NOT NULL,
  cover_key TEXT NOT NULL DEFAULT '',
  archived_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  platform TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS reading_progress (
  book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  locator TEXT NOT NULL,
  percentage REAL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (book_id, device_id)
);
`,
	},
	{
		Version: 2,
		Name:    "settings",
		SQL: `
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`,
	},
	{
		Version: 3,
		Name:    "reading_progress_devices",
		SQL: `
ALTER TABLE books ADD COLUMN content_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN system_name TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN manufacturer TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN app_version TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN disabled_at TEXT;
ALTER TABLE reading_progress ADD COLUMN content_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE reading_progress ADD COLUMN client_updated_at TEXT;
CREATE TABLE reading_daily (
  book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  reading_date TEXT NOT NULL,
  read_seconds INTEGER NOT NULL CHECK (read_seconds >= 0),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (book_id, device_id, reading_date)
);
CREATE INDEX reading_progress_updated_idx ON reading_progress(updated_at DESC);
CREATE INDEX reading_daily_device_date_idx ON reading_daily(device_id, reading_date DESC);
CREATE INDEX reading_daily_book_date_idx ON reading_daily(book_id, reading_date DESC);
`,
	},
}

func OpenAndMigrate(ctx context.Context, databasePath string) (*sql.DB, error) {
	if err := ensureParentDir(databasePath); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if err := RunMigrations(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func RunMigrations(ctx context.Context, conn *sql.DB) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
);
`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedMigrations(ctx, tx)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %d %s: %w", migration.Version, migration.Name, err)
		}
		if migration.Version == 3 {
			if _, err := tx.ExecContext(ctx, `UPDATE books SET content_revision = ? WHERE content_revision = ''`, now); err != nil {
				return fmt.Errorf("backfill book content revisions: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`, migration.Version, migration.Name, now); err != nil {
			return fmt.Errorf("record migration %d %s: %w", migration.Version, migration.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func appliedMigrations(ctx context.Context, tx *sql.Tx) (map[int]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}

func ensureParentDir(databasePath string) error {
	if databasePath == "" {
		return errors.New("database path is required")
	}
	if databasePath == ":memory:" {
		return nil
	}
	parent := filepath.Dir(databasePath)
	if parent == "." || parent == "" {
		return nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create database parent directory %q: %w", parent, err)
	}
	return nil
}
