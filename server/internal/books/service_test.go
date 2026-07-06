package books

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/amwangfan/omnireader/server/internal/db"
	"github.com/amwangfan/omnireader/server/internal/storage"
	_ "modernc.org/sqlite"
)

func TestCreateListOpenAndArchiveBook(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)

	book, err := service.Create(ctx, CreateInput{
		Filename: "The Book.epub",
		Body:     strings.NewReader(string(fixtureEPUB(t, "The Parsed Book", "The Parsed Author"))),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if book.Title != "The Parsed Book" || book.Author != "The Parsed Author" || book.Format != "epub" {
		t.Fatalf("unexpected book: %#v", book)
	}
	wantRevision := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	if !book.ContentRevision.Equal(wantRevision) {
		t.Fatalf("ContentRevision = %v, want %v", book.ContentRevision, wantRevision)
	}
	encoded, err := json.Marshal(book)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"contentRevision":"2026-07-04T10:00:00.000000000Z"`) {
		t.Fatalf("book JSON does not preserve fixed revision precision: %s", encoded)
	}
	if !strings.HasSuffix(book.StorageKey, "The Parsed Book-The Parsed Author.epub") {
		t.Fatalf("storage key = %q", book.StorageKey)
	}

	books, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(books) != 1 || books[0].ID != book.ID {
		t.Fatalf("unexpected books: %#v", books)
	}
	if !books[0].ContentRevision.Equal(wantRevision) {
		t.Fatalf("listed ContentRevision = %v, want %v", books[0].ContentRevision, wantRevision)
	}

	_, reader, err := service.Open(ctx, book.ID)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("downloaded body should not be empty")
	}

	if err := service.Delete(ctx, book.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	books, err = service.List(ctx)
	if err != nil {
		t.Fatalf("List after delete returned error: %v", err)
	}
	if len(books) != 0 {
		t.Fatalf("deleted book should be hidden: %#v", books)
	}
	if _, _, err := service.Open(ctx, book.ID); err == nil {
		t.Fatal("deleted book should not open")
	}
}

func TestCreateRejectsNonEPUB(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)

	if _, err := service.Create(ctx, CreateInput{Filename: "book.pdf", Body: strings.NewReader("pdf")}); err == nil {
		t.Fatal("expected non-EPUB upload to fail")
	}
}

func TestCreateUsesCustomFilenameTemplate(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	if err := service.SetFilenameTemplate(ctx, "{{YYMMDD}}-{{Book}}-{{Author}}-123"); err != nil {
		t.Fatalf("SetFilenameTemplate returned error: %v", err)
	}

	book, err := service.Create(ctx, CreateInput{
		Filename: "fallback.epub",
		Body:     strings.NewReader(string(fixtureEPUB(t, "Book", "Author"))),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if !strings.HasSuffix(book.StorageKey, "260704-Book-Author-123.epub") {
		t.Fatalf("storage key = %q", book.StorageKey)
	}
}

func TestUpdateDetailsRenamesStoredFile(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{
		Filename: "fallback.epub",
		Body:     strings.NewReader(string(fixtureEPUB(t, "Old Title", "Old Author"))),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	updated, err := service.UpdateDetails(ctx, book.ID, UpdateInput{
		Title:    "New Title",
		Author:   "New Author",
		Filename: "custom-name",
	})
	if err != nil {
		t.Fatalf("UpdateDetails returned error: %v", err)
	}
	if updated.Title != "New Title" || updated.Author != "New Author" || updated.Filename != "custom-name.epub" {
		t.Fatalf("unexpected updated book: %#v", updated)
	}
	if !strings.HasSuffix(updated.StorageKey, "/custom-name.epub") {
		t.Fatalf("storage key = %q", updated.StorageKey)
	}
	_, reader, err := service.Open(ctx, book.ID)
	if err != nil {
		t.Fatalf("renamed book should open: %v", err)
	}
	_ = reader.Close()
	revisions, err := service.ListRevisions(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].StorageKey != updated.StorageKey {
		t.Fatalf("renamed current revision key = %#v, want %q", revisions, updated.StorageKey)
	}
	revisionReader, err := service.store.Open(ctx, revisions[0].StorageKey)
	if err != nil {
		t.Fatalf("renamed revision object should open: %v", err)
	}
	_ = revisionReader.Close()
}

func TestUpdateDetailsRequiresTitle(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{
		Filename: "fallback.epub",
		Body:     strings.NewReader(string(fixtureEPUB(t, "Title", "Author"))),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if _, err := service.UpdateDetails(ctx, book.ID, UpdateInput{Title: "   "}); err == nil {
		t.Fatal("expected blank title to fail")
	}
}

func TestDeleteRemovesDailyReadingRows(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUB(t, "Book", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	now := "2026-07-04T10:00:00Z"
	if _, err := service.db.Exec(`INSERT INTO devices (id,display_name,platform,last_seen_at,created_at,updated_at) VALUES ('11111111-1111-4111-8111-111111111111','Device','android',?,?,?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.Exec(`INSERT INTO reading_daily (book_id,device_id,reading_date,read_seconds,updated_at) VALUES (?,'11111111-1111-4111-8111-111111111111','2026-07-04',10,?)`, book.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, book.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := service.db.QueryRow(`SELECT COUNT(*) FROM reading_daily WHERE book_id=?`, book.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("daily rows after delete = %d, err=%v", count, err)
	}
}

func testService(t *testing.T, ctx context.Context) *Service {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.RunMigrations(ctx, conn); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal returned error: %v", err)
	}
	service, err := NewService(conn, store, Options{
		Now: func() time.Time {
			return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	return service
}
