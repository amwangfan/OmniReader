package books

import (
	"context"
	"database/sql"
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
