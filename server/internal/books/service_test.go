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
	if book.Title != "The Parsed Book" || book.Author != "The Parsed Author" || book.Format != "epub" || book.SourceFormat != "epub" {
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

	if err := service.Archive(ctx, book.ID); err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	books, err = service.List(ctx)
	if err != nil {
		t.Fatalf("List after delete returned error: %v", err)
	}
	if len(books) != 0 {
		t.Fatalf("archived book should be hidden: %#v", books)
	}
	if _, _, err := service.Open(ctx, book.ID); err == nil {
		t.Fatal("archived book should not open through the active library")
	}
	stored, err := service.store.Open(ctx, book.StorageKey)
	if err != nil {
		t.Fatalf("archived epub should remain recoverable: %v", err)
	}
	_ = stored.Close()
}

func TestCreateRejectsUnsupportedFormat(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)

	if _, err := service.Create(ctx, CreateInput{Filename: "book.docx", Body: strings.NewReader("docx")}); err == nil {
		t.Fatal("expected unsupported upload to fail")
	}
}

func TestCreateConvertsSupportedSourceToEPUB(t *testing.T) {
	ctx := context.Background()
	converted := fixtureEPUB(t, "Converted PDF", "Converted Author")
	service := testServiceWithConverter(t, ctx, &fakeConverter{output: converted})

	book, err := service.Create(ctx, CreateInput{Filename: "source.pdf", Body: strings.NewReader("%PDF fixture")})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if book.Format != "epub" || book.SourceFormat != "pdf" || book.Title != "Converted PDF" {
		t.Fatalf("unexpected converted book: %#v", book)
	}
	result, err := service.Search(ctx, "converted author")
	if err != nil || len(result) != 1 || result[0].ID != book.ID {
		t.Fatalf("search result = %#v, err = %v", result, err)
	}
}

func TestCreateRejectsInvalidEPUBArchive(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	_, err := service.Create(ctx, CreateInput{
		Filename: "not-a-book.epub",
		Body:     strings.NewReader("not a zip archive"),
	})
	if err == nil {
		t.Fatal("expected invalid epub archive to fail")
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

func testService(t *testing.T, ctx context.Context) *Service {
	return testServiceWithConverter(t, ctx, nil)
}

func testServiceWithConverter(t *testing.T, ctx context.Context, converter Converter) *Service {
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
		Converter: converter,
		Now: func() time.Time {
			return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	return service
}

type fakeConverter struct {
	output []byte
	err    error
}

func (f *fakeConverter) Convert(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return f.output, f.err
}

func (f *fakeConverter) Status() ConversionStatus {
	return ConversionStatus{Engine: "fake", Available: true, SupportedFormats: supportedSourceFormats}
}
