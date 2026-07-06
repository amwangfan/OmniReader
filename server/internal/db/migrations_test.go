package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAndMigrateCreatesCoreTables(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "nested", "app.db")

	conn, err := OpenAndMigrate(ctx, databasePath)
	if err != nil {
		t.Fatalf("OpenAndMigrate returned error: %v", err)
	}
	defer conn.Close()

	for _, table := range []string{"users", "sessions", "books", "devices", "reading_progress", "reading_daily", "settings", "schema_migrations"} {
		if !tableExists(t, conn, table) {
			t.Fatalf("expected table %q to exist", table)
		}
	}
	for table, columns := range map[string][]string{
		"books":            {"content_revision"},
		"devices":          {"system_name", "manufacturer", "model", "app_version", "disabled_at"},
		"reading_progress": {"content_revision", "client_updated_at"},
	} {
		for _, column := range columns {
			if !columnExists(t, conn, table, column) {
				t.Fatalf("expected column %s.%s to exist", table, column)
			}
		}
	}
}

func TestMigrationThreeBackfillsBookContentRevision(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, migrations[0].SQL+migrations[1].SQL); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	legacyTime := "2026-07-05T01:02:03.123456789Z"
	if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (1, 'core_schema', ?), (2, 'settings', ?)`, legacyTime, legacyTime); err != nil {
		t.Fatalf("record legacy migrations: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO books (id,title,format,storage_key,file_size,checksum,created_at,updated_at) VALUES ('book','Book','epub','book.epub',1,'sum',?,?)`, legacyTime, legacyTime); err != nil {
		t.Fatalf("insert legacy book: %v", err)
	}
	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	var revision string
	if err := conn.QueryRowContext(ctx, `SELECT content_revision FROM books WHERE id='book'`).Scan(&revision); err != nil {
		t.Fatalf("read content revision: %v", err)
	}
	if revision != legacyTime {
		t.Fatalf("content revision = %q, want %q", revision, legacyTime)
	}
	if _, err := time.Parse(time.RFC3339Nano, revision); err != nil {
		t.Fatalf("content revision is not RFC3339Nano: %v", err)
	}
}

func TestRunMigrationsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	defer conn.Close()

	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("first RunMigrations returned error: %v", err)
	}
	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("second RunMigrations returned error: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&count); err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration version 1 recorded %d times, want 1", count)
	}
}

func tableExists(t *testing.T, conn *sql.DB, table string) bool {
	t.Helper()

	var name string
	err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query table %q: %v", table, err)
	}
	return name == table
}

func columnExists(t *testing.T, conn *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("inspect table %q: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}
