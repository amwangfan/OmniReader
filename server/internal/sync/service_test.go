package sync

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/amwangfan/omnireader/server/internal/db"
	_ "modernc.org/sqlite"
)

func TestDeviceAndProgressLifecycle(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.RunMigrations(ctx, conn); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	service, err := NewService(conn, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO books (id, title, author, format, storage_key, file_size, checksum, created_at, updated_at) VALUES (?, ?, '', 'epub', ?, 1, 'sum', ?, ?)`, "book_1", "Book", "books/book_1/book.epub", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	device, err := service.UpsertDevice(ctx, UpsertDeviceInput{ID: "device_1", DisplayName: "BOOX", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	if device.DisplayName != "BOOX" {
		t.Fatalf("device = %#v", device)
	}

	percentage := 0.25
	progress, err := service.PutProgress(ctx, PutProgressInput{BookID: "book_1", DeviceID: "device_1", Locator: "chapter:2", Percentage: &percentage, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if progress.Locator != "chapter:2" || progress.Percentage == nil || *progress.Percentage != percentage {
		t.Fatalf("progress = %#v", progress)
	}

	older := now.Add(-time.Minute)
	if _, err := service.PutProgress(ctx, PutProgressInput{BookID: "book_1", DeviceID: "device_1", Locator: "chapter:1", UpdatedAt: older}); err != nil {
		t.Fatal(err)
	}
	latest, err := service.GetLatestProgress(ctx, "book_1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Locator != "chapter:2" {
		t.Fatalf("older progress replaced newer progress: %#v", latest)
	}
	activities, err := service.ListRecentProgress(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 || activities[0].BookTitle != "Book" || activities[0].DeviceName != "BOOX" || activities[0].Locator != "chapter:2" {
		t.Fatalf("activities = %#v", activities)
	}
}

func TestPutProgressValidatesPercentage(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	service, err := NewService(conn, Options{})
	if err != nil {
		t.Fatal(err)
	}
	percentage := 1.1
	if _, err := service.PutProgress(context.Background(), PutProgressInput{BookID: "book", DeviceID: "device", Locator: "chapter:1", Percentage: &percentage}); err == nil {
		t.Fatal("expected invalid percentage to fail")
	}
}
