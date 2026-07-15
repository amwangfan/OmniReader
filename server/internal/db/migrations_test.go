package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrateCreatesCoreTables(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "nested", "app.db")

	conn, err := OpenAndMigrate(ctx, databasePath)
	if err != nil {
		t.Fatalf("OpenAndMigrate returned error: %v", err)
	}
	defer conn.Close()

	for _, table := range []string{"users", "sessions", "books", "devices", "reading_progress", "settings", "schema_migrations"} {
		if !tableExists(t, conn, table) {
			t.Fatalf("expected table %q to exist", table)
		}
	}
	if !columnExists(t, conn, "books", "source_format") {
		t.Fatal("expected books.source_format column")
	}
	if _, err := conn.Exec(`INSERT INTO books (id, title, format, storage_key, file_size, checksum, created_at, updated_at) VALUES ('book_1', 'Book', 'epub', 'books/book_1/book.epub', 1, 'sum', '2026-07-14T00:00:00Z', '2026-07-14T00:00:00Z')`); err != nil {
		t.Fatalf("insert book with legacy columns: %v", err)
	}
	var sourceFormat string
	if err := conn.QueryRow(`SELECT source_format FROM books WHERE id = 'book_1'`).Scan(&sourceFormat); err != nil || sourceFormat != "epub" {
		t.Fatalf("source format = %q, err = %v", sourceFormat, err)
	}
}

func columnExists(t *testing.T, conn *sql.DB, table string, column string) bool {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("query columns for %q: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan column for %q: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	return false
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
