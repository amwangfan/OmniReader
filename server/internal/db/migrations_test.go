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
