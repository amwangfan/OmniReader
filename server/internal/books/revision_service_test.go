package books

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/amwangfan/omnireader/server/internal/epubedit"
	"github.com/amwangfan/omnireader/server/internal/storage"
)

func TestCreateRecordsImmutableOriginalRevision(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := service.ListRevisions(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || !revisions[0].Original || revisions[0].StorageKey != book.StorageKey {
		t.Fatalf("revisions = %#v", revisions)
	}
}

func TestPublishMetadataRejectsStaleBaseWithoutChangingCurrent(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateContentMetadata(ctx, book.ID, MetadataMutation{BaseRevision: "2026-01-01T00:00:00.000000001Z", Metadata: epubedit.Metadata{Title: "Wrong", Author: "Author"}})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.CurrentRevision != formatTime(book.ContentRevision.Time) {
		t.Fatalf("stale error = %#v", err)
	}
	current, reader, err := service.Open(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	_, _ = io.ReadAll(reader)
	if current.Title != "Original" || current.Checksum != book.Checksum || current.StorageKey != book.StorageKey {
		t.Fatalf("current changed: %#v", current)
	}
}

func TestPublishMetadataCreatesRevisionAndLeavesReadingProgress(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	now := "2026-07-04T10:00:00.000000000Z"
	device := "11111111-1111-4111-8111-111111111111"
	_, err = service.db.Exec(`INSERT INTO devices (id,display_name,platform,last_seen_at,created_at,updated_at) VALUES (?, 'Device','android',?,?,?)`, device, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.db.Exec(`INSERT INTO reading_progress (book_id,device_id,locator,content_revision,updated_at) VALUES (?,?,?,?,?)`, book.ID, device, `{"chapterId":"chapter-one"}`, formatTime(book.ContentRevision.Time), now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.UpdateContentMetadata(ctx, book.ID, MetadataMutation{BaseRevision: formatTime(book.ContentRevision.Time), Metadata: epubedit.Metadata{Title: "Changed", Author: "Writer", Language: "en"}, Summary: "edit metadata"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Book.Title != "Changed" || result.Revision.Original || result.Revision.ChangeType != "metadata" || !result.Book.ContentRevision.After(book.ContentRevision.Time) {
		t.Fatalf("result=%#v", result)
	}
	var locator, progressRevision string
	if err := service.db.QueryRow(`SELECT locator,content_revision FROM reading_progress WHERE book_id=? AND device_id=?`, book.ID, device).Scan(&locator, &progressRevision); err != nil {
		t.Fatal(err)
	}
	if locator != `{"chapterId":"chapter-one"}` || progressRevision != formatTime(book.ContentRevision.Time) {
		t.Fatalf("progress changed: %s %s", locator, progressRevision)
	}
}

func TestConcurrentPublishAllowsOneWriter(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes, conflicts int
	var lock sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := service.UpdateContentMetadata(ctx, book.ID, MetadataMutation{BaseRevision: formatTime(book.ContentRevision.Time), Metadata: epubedit.Metadata{Title: fmt.Sprintf("Writer %d", i), Author: "A"}})
			lock.Lock()
			defer lock.Unlock()
			if err == nil {
				successes++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Errorf("publish: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestRevisionRetentionAndRestoreAsNew(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	original := formatTime(book.ContentRevision.Time)
	base := original
	for i := 0; i < 7; i++ {
		result, err := service.UpdateContentMetadata(ctx, book.ID, MetadataMutation{BaseRevision: base, Metadata: epubedit.Metadata{Title: fmt.Sprintf("Edit %d", i), Author: "Author"}})
		if err != nil {
			t.Fatal(err)
		}
		base = formatTime(result.Book.ContentRevision.Time)
	}
	revisions, err := service.ListRevisions(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 6 {
		t.Fatalf("retained %d revisions, want original plus five", len(revisions))
	}
	foundOriginal := false
	for _, revision := range revisions {
		foundOriginal = foundOriginal || revision.Original
	}
	if !foundOriginal {
		t.Fatal("original revision was pruned")
	}
	restored, err := service.RestoreRevision(ctx, book.ID, RestoreInput{BaseRevision: base, Revision: original, Summary: "restore original"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Book.Title != "Original" || formatTime(restored.Book.ContentRevision.Time) == original || restored.Revision.ChangeType != "restore" {
		t.Fatalf("restore=%#v", restored)
	}
}

func TestInvalidMutationAndDatabaseFailureLeaveCurrentUnchanged(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	base := formatTime(book.ContentRevision.Time)
	_, err = service.UpdateContentChapter(ctx, book.ID, ChapterMutation{BaseRevision: base, ChapterID: "chapter-one", Source: `<html><body>`})
	if !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("invalid XHTML error = %v", err)
	}
	current, err := service.Get(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Checksum != book.Checksum || formatTime(current.ContentRevision.Time) != base {
		t.Fatal("invalid mutation changed current book")
	}
	tracker := &trackingStore{Store: service.store}
	service.store = tracker
	if _, err := service.db.Exec(`CREATE TRIGGER reject_generated_revision BEFORE INSERT ON book_revisions WHEN NEW.is_original=0 BEGIN SELECT RAISE(FAIL,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateContentMetadata(ctx, book.ID, MetadataMutation{BaseRevision: base, Metadata: epubedit.Metadata{Title: "Changed", Author: "Author"}})
	if err == nil {
		t.Fatal("expected database failure")
	}
	current, err = service.Get(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Checksum != book.Checksum || formatTime(current.ContentRevision.Time) != base {
		t.Fatal("database failure changed current book")
	}
	if len(tracker.saved) == 0 || len(tracker.deleted) != len(tracker.saved) {
		t.Fatalf("saved=%v deleted=%v", tracker.saved, tracker.deleted)
	}
}

func TestRevisionPruningKeepsSharedCurrentCoverObject(t *testing.T) {
	ctx := context.Background()
	service := testService(t, ctx)
	book, err := service.Create(ctx, CreateInput{Filename: "book.epub", Body: strings.NewReader(string(fixtureEPUBWithSpine(t, "Original", "Author")))})
	if err != nil {
		t.Fatal(err)
	}
	var cover bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	covered, err := service.ReplaceContentCover(ctx, book.ID, CoverMutation{BaseRevision: formatTime(book.ContentRevision.Time), Data: cover.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	base := formatTime(covered.Book.ContentRevision.Time)
	coverKey := covered.Book.CoverKey
	for i := 0; i < 7; i++ {
		result, err := service.UpdateContentMetadata(ctx, book.ID, MetadataMutation{BaseRevision: base, Metadata: epubedit.Metadata{Title: fmt.Sprintf("Edit %d", i), Author: "Author"}})
		if err != nil {
			t.Fatal(err)
		}
		base = formatTime(result.Book.ContentRevision.Time)
	}
	reader, err := service.store.Open(ctx, coverKey)
	if err != nil {
		t.Fatalf("shared current cover was pruned: %v", err)
	}
	_ = reader.Close()
	current, err := service.Get(ctx, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CoverKey != coverKey {
		t.Fatalf("current cover key=%q want %q", current.CoverKey, coverKey)
	}
}

type trackingStore struct {
	storage.Store
	saved, deleted []string
}

func (s *trackingStore) Save(ctx context.Context, key string, body io.Reader) error {
	s.saved = append(s.saved, key)
	return s.Store.Save(ctx, key, body)
}
func (s *trackingStore) Delete(ctx context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.Store.Delete(ctx, key)
}

func fixtureEPUBWithSpine(t *testing.T, title, author string) []byte {
	t.Helper()
	return rawBookEPUB(t, map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`,
		"OPS/content.opf":        `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title><dc:creator>` + author + `</dc:creator></metadata><manifest><item id="chapter-one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter-one"/></spine></package>`,
		"OPS/one.xhtml":          `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><p>Text</p></body></html>`,
	})
}

func rawBookEPUB(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
