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

	for _, table := range []string{"users", "sessions", "books", "book_revisions", "devices", "reading_progress", "reading_daily", "settings", "schema_migrations"} {
		if !tableExists(t, conn, table) {
			t.Fatalf("expected table %q to exist", table)
		}
	}
	for table, columns := range map[string][]string{
		"books":            {"content_revision", "cover_media_type", "cover_width", "cover_height"},
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

func TestMigrationFourBackfillsOneImmutableOriginalAndCascades(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, migrations[0].SQL+migrations[1].SQL+migrations[2].SQL); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (1,'core','x'),(2,'settings','x'),(3,'reading','x')`); err != nil {
		t.Fatal(err)
	}
	const revision = "2026-07-06T01:02:03.123456789Z"
	if _, err := conn.ExecContext(ctx, `INSERT INTO books (id,title,format,storage_key,file_size,checksum,content_revision,created_at,updated_at) VALUES ('book','Book','epub','books/book/original.epub',123,'abc',?,?,?)`, revision, revision, revision); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("idempotent RunMigrations: %v", err)
	}
	var count, original int
	var key, checksum string
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*), SUM(is_original), storage_key, checksum FROM book_revisions WHERE book_id='book'`).Scan(&count, &original, &key, &checksum); err != nil {
		t.Fatal(err)
	}
	if count != 1 || original != 1 || key != "books/book/original.epub" || checksum != "abc" {
		t.Fatalf("backfill = count %d original %d key %q checksum %q", count, original, key, checksum)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO book_revisions (book_id,revision,storage_key,checksum,file_size,change_type,change_summary,is_original,created_at) VALUES ('book','2026-07-06T01:02:04.123456789Z','x','x',1,'upload','',1,?)`, revision); err == nil {
		t.Fatal("expected a second original revision to violate uniqueness")
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM books WHERE id='book'`); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM book_revisions WHERE book_id='book'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("revision rows after parent deletion = %d, err=%v", count, err)
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
	if _, err := conn.ExecContext(ctx, `
INSERT INTO devices (id,display_name,platform,last_seen_at,created_at,updated_at) VALUES
 ('11111111-1111-4111-8111-111111111111','A','android','2026-07-05T01:02:03.12Z','2026-07-05T01:02:03.12Z','2026-07-05T01:02:03.12Z'),
 ('22222222-2222-4222-8222-222222222222','B','android','2026-07-05T01:02:03.123Z','2026-07-05T01:02:03.123Z','2026-07-05T01:02:03.123Z')`); err != nil {
		t.Fatalf("insert legacy devices: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO reading_progress (book_id,device_id,locator,percentage,updated_at) VALUES
 ('book','11111111-1111-4111-8111-111111111111','{}',0.1,'2026-07-05T01:02:03.12Z'),
 ('book','22222222-2222-4222-8222-222222222222','{}',0.2,'2026-07-05T01:02:03.123Z')`); err != nil {
		t.Fatalf("insert legacy progress: %v", err)
	}
	before := time.Now().UTC()
	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	after := time.Now().UTC()
	var revision string
	if err := conn.QueryRowContext(ctx, `SELECT content_revision FROM books WHERE id='book'`).Scan(&revision); err != nil {
		t.Fatalf("read content revision: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, revision)
	if err != nil {
		t.Fatalf("content revision is not RFC3339Nano: %v", err)
	}
	if revision == legacyTime || parsed.Before(before) || parsed.After(after) {
		t.Fatalf("content revision = %q, want migration execution time between %v and %v", revision, before, after)
	}
	if len(revision) != len("2026-07-06T05:18:26.123456789Z") {
		t.Fatalf("content revision %q is not fixed 9-digit UTC format", revision)
	}
	var latestDevice, latestUpdated string
	if err := conn.QueryRowContext(ctx, `SELECT device_id, updated_at FROM reading_progress ORDER BY updated_at DESC LIMIT 1`).Scan(&latestDevice, &latestUpdated); err != nil {
		t.Fatalf("read latest migrated progress: %v", err)
	}
	if latestDevice != "22222222-2222-4222-8222-222222222222" || latestUpdated != "2026-07-05T01:02:03.123000000Z" {
		t.Fatalf("latest migrated progress = %s at %s", latestDevice, latestUpdated)
	}
	var deviceTimes []string
	rows, err := conn.QueryContext(ctx, `SELECT last_seen_at || '|' || created_at || '|' || updated_at FROM devices ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		deviceTimes = append(deviceTimes, value)
	}
	if len(deviceTimes) != 2 || deviceTimes[0] != "2026-07-05T01:02:03.120000000Z|2026-07-05T01:02:03.120000000Z|2026-07-05T01:02:03.120000000Z" || deviceTimes[1] != "2026-07-05T01:02:03.123000000Z|2026-07-05T01:02:03.123000000Z|2026-07-05T01:02:03.123000000Z" {
		t.Fatalf("migrated device times = %#v", deviceTimes)
	}
}

func TestOpenAndMigrateEnablesForeignKeys(t *testing.T) {
	conn, err := OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "foreign.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var enabled int
	if err := conn.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign_keys = %d, err = %v", enabled, err)
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
