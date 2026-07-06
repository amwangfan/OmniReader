package books

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/amwangfan/omnireader/server/internal/epubedit"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("revision conflict")
	ErrInvalidContent = errors.New("invalid content")
)

type ConflictError struct {
	CurrentRevision string `json:"currentRevision"`
}

func (e *ConflictError) Error() string {
	return "content revision conflict; current revision is " + e.CurrentRevision
}
func (e *ConflictError) Unwrap() error { return ErrConflict }

type InvalidContentError struct{ Cause error }

func (e *InvalidContentError) Error() string { return e.Cause.Error() }
func (e *InvalidContentError) Unwrap() error { return ErrInvalidContent }

type Revision struct {
	BookID          string    `json:"bookId"`
	Revision        string    `json:"revision"`
	StorageKey      string    `json:"-"`
	CoverKey        string    `json:"-"`
	CoverMediaType  string    `json:"coverMediaType,omitempty"`
	CoverWidth      int       `json:"coverWidth,omitempty"`
	CoverHeight     int       `json:"coverHeight,omitempty"`
	ContentIndexKey string    `json:"-"`
	Checksum        string    `json:"checksum"`
	FileSize        int64     `json:"fileSize"`
	ChangeType      string    `json:"changeType"`
	ChangeSummary   string    `json:"changeSummary"`
	Original        bool      `json:"original"`
	CreatedAt       time.Time `json:"createdAt"`
}

type PublishResult struct {
	Book     Book     `json:"book"`
	Revision Revision `json:"revision"`
}
type ContentView struct {
	Book    Book             `json:"book"`
	Content epubedit.Content `json:"content"`
}
type ChapterView struct {
	BookID          string           `json:"bookId"`
	ContentRevision string           `json:"contentRevision"`
	Chapter         epubedit.Chapter `json:"chapter"`
	Source          string           `json:"source"`
}

type MetadataMutation struct {
	BaseRevision string
	Metadata     epubedit.Metadata
	Summary      string
}
type ChapterMutation struct {
	BaseRevision string
	ChapterID    string
	Source       string
	Title        string
	Summary      string
}
type AddChapterMutation struct {
	BaseRevision string
	Source       string
	Title        string
	Summary      string
}
type DeleteChapterMutation struct {
	BaseRevision string
	ChapterID    string
	Summary      string
}
type SpineMutation struct {
	BaseRevision string
	ChapterIDs   []string
	Summary      string
}
type CoverMutation struct {
	BaseRevision string
	Data         []byte
	Limits       epubedit.CoverLimits
	Summary      string
}
type RestoreInput struct {
	BaseRevision string
	Revision     string
	Summary      string
}

func (s *Service) GetContent(ctx context.Context, bookID string) (ContentView, error) {
	book, data, err := s.readCurrent(ctx, bookID)
	if err != nil {
		return ContentView{}, err
	}
	workspace, err := epubedit.Open(data, epubedit.Limits{})
	if err != nil {
		return ContentView{}, mapContentError(err)
	}
	defer workspace.Close()
	return ContentView{Book: book, Content: workspace.Content()}, nil
}

func (s *Service) GetChapter(ctx context.Context, bookID, chapterID string) (ChapterView, error) {
	book, data, err := s.readCurrent(ctx, bookID)
	if err != nil {
		return ChapterView{}, err
	}
	workspace, err := epubedit.Open(data, epubedit.Limits{})
	if err != nil {
		return ChapterView{}, mapContentError(err)
	}
	defer workspace.Close()
	var chapter epubedit.Chapter
	found := false
	for _, candidate := range workspace.Content().Chapters {
		if candidate.ID == chapterID {
			chapter = candidate
			found = true
			break
		}
	}
	if !found {
		return ChapterView{}, ErrNotFound
	}
	source, err := workspace.ChapterSource(chapterID)
	if err != nil {
		return ChapterView{}, mapContentError(err)
	}
	return ChapterView{BookID: book.ID, ContentRevision: formatTime(book.ContentRevision.Time), Chapter: chapter, Source: source}, nil
}

func (s *Service) UpdateContentMetadata(ctx context.Context, bookID string, input MetadataMutation) (PublishResult, error) {
	if strings.TrimSpace(input.Metadata.Title) == "" {
		return PublishResult{}, &InvalidContentError{Cause: errors.New("title is required")}
	}
	return s.publish(ctx, bookID, input.BaseRevision, "metadata", input.Summary, nil, func(workspace *epubedit.Workspace) (*epubedit.CoverInfo, error) {
		return nil, workspace.UpdateMetadata(input.Metadata)
	})
}
func (s *Service) UpdateContentChapter(ctx context.Context, bookID string, input ChapterMutation) (PublishResult, error) {
	return s.publish(ctx, bookID, input.BaseRevision, "chapter_update", input.Summary, nil, func(w *epubedit.Workspace) (*epubedit.CoverInfo, error) {
		return nil, w.UpdateChapter(input.ChapterID, input.Source, input.Title)
	})
}
func (s *Service) AddContentChapter(ctx context.Context, bookID string, input AddChapterMutation) (PublishResult, error) {
	return s.publish(ctx, bookID, input.BaseRevision, "chapter_add", input.Summary, nil, func(w *epubedit.Workspace) (*epubedit.CoverInfo, error) {
		_, err := w.AddChapter(input.Title, input.Source)
		return nil, err
	})
}
func (s *Service) DeleteContentChapter(ctx context.Context, bookID string, input DeleteChapterMutation) (PublishResult, error) {
	return s.publish(ctx, bookID, input.BaseRevision, "chapter_delete", input.Summary, nil, func(w *epubedit.Workspace) (*epubedit.CoverInfo, error) { return nil, w.DeleteChapter(input.ChapterID) })
}
func (s *Service) ReorderContentSpine(ctx context.Context, bookID string, input SpineMutation) (PublishResult, error) {
	return s.publish(ctx, bookID, input.BaseRevision, "spine_reorder", input.Summary, nil, func(w *epubedit.Workspace) (*epubedit.CoverInfo, error) { return nil, w.ReorderSpine(input.ChapterIDs) })
}
func (s *Service) ReplaceContentCover(ctx context.Context, bookID string, input CoverMutation) (PublishResult, error) {
	return s.publish(ctx, bookID, input.BaseRevision, "cover", input.Summary, nil, func(w *epubedit.Workspace) (*epubedit.CoverInfo, error) {
		cover, err := w.ReplaceCover(input.Data, input.Limits)
		return &cover, err
	})
}

type sourceRevision struct {
	data                     []byte
	coverKey, coverMediaType string
	coverWidth, coverHeight  int
}
type mutateWorkspace func(*epubedit.Workspace) (*epubedit.CoverInfo, error)

func (s *Service) publish(ctx context.Context, bookID, baseRevision, changeType, summary string, source *sourceRevision, mutate mutateWorkspace) (PublishResult, error) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	book, currentData, err := s.readCurrent(ctx, bookID)
	if err != nil {
		return PublishResult{}, err
	}
	currentRevision := formatTime(book.ContentRevision.Time)
	if baseRevision != currentRevision {
		return PublishResult{}, &ConflictError{CurrentRevision: currentRevision}
	}
	data := currentData
	if source != nil {
		data = source.data
	}
	workspace, err := epubedit.Open(data, epubedit.Limits{})
	if err != nil {
		return PublishResult{}, mapContentError(err)
	}
	defer workspace.Close()
	cover, err := mutate(workspace)
	if err != nil {
		return PublishResult{}, mapContentError(err)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		return PublishResult{}, mapContentError(err)
	}
	content := workspace.Content()
	revisionTime := nextRevision(book.ContentRevision.Time, s.now())
	revision := formatTime(revisionTime)
	storageKey := revisionStorageKey(book, revision)
	if err := s.store.Save(ctx, storageKey, bytes.NewReader(rebuilt)); err != nil {
		return PublishResult{}, fmt.Errorf("save EPUB revision: %w", err)
	}
	cleanupKeys := []string{storageKey}
	coverKey, coverMediaType, coverWidth, coverHeight := book.CoverKey, book.CoverMediaType, book.CoverWidth, book.CoverHeight
	if source != nil {
		coverKey, coverMediaType, coverWidth, coverHeight = source.coverKey, source.coverMediaType, source.coverWidth, source.coverHeight
	}
	if cover != nil && len(cover.Data) > 0 {
		coverKey = coverStorageKey(book.ID, revision, cover.MediaType)
		if err := s.store.Save(ctx, coverKey, bytes.NewReader(cover.Data)); err != nil {
			s.deleteObjects(ctx, cleanupKeys)
			return PublishResult{}, fmt.Errorf("save cover preview: %w", err)
		}
		cleanupKeys = append(cleanupKeys, coverKey)
		coverMediaType, coverWidth, coverHeight = cover.MediaType, cover.Width, cover.Height
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.deleteObjects(ctx, cleanupKeys)
		return PublishResult{}, fmt.Errorf("begin publication: %w", err)
	}
	defer tx.Rollback()
	created := formatTime(revisionTime)
	sum := checksum(rebuilt)
	_, err = tx.ExecContext(ctx, `INSERT INTO book_revisions (book_id,revision,storage_key,cover_key,cover_media_type,cover_width,cover_height,checksum,file_size,change_type,change_summary,is_original,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,0,?)`, book.ID, revision, storageKey, coverKey, coverMediaType, coverWidth, coverHeight, sum, len(rebuilt), changeType, strings.TrimSpace(summary), created)
	if err != nil {
		_ = tx.Rollback()
		s.deleteObjects(ctx, cleanupKeys)
		if latest := s.currentRevision(ctx, book.ID); latest != "" && latest != currentRevision {
			return PublishResult{}, &ConflictError{CurrentRevision: latest}
		}
		return PublishResult{}, fmt.Errorf("insert book revision: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE books SET title=?,author=?,storage_key=?,file_size=?,checksum=?,cover_key=?,cover_media_type=?,cover_width=?,cover_height=?,content_revision=?,updated_at=? WHERE id=? AND archived_at IS NULL AND content_revision=?`, content.Metadata.Title, content.Metadata.Author, storageKey, len(rebuilt), sum, coverKey, coverMediaType, coverWidth, coverHeight, revision, created, book.ID, currentRevision)
	if err != nil {
		s.deleteObjects(ctx, cleanupKeys)
		return PublishResult{}, fmt.Errorf("advance current book revision: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		_ = tx.Rollback()
		s.deleteObjects(ctx, cleanupKeys)
		return PublishResult{}, &ConflictError{CurrentRevision: s.currentRevision(ctx, book.ID)}
	}
	if err := tx.Commit(); err != nil {
		s.deleteObjects(ctx, cleanupKeys)
		return PublishResult{}, fmt.Errorf("commit publication: %w", err)
	}
	updated, err := s.Get(ctx, book.ID)
	if err != nil {
		return PublishResult{}, err
	}
	entry, err := s.revision(ctx, book.ID, revision)
	if err != nil {
		return PublishResult{}, err
	}
	s.pruneRevisions(ctx, book.ID, revision)
	return PublishResult{Book: updated, Revision: entry}, nil
}

func (s *Service) ListRevisions(ctx context.Context, bookID string) ([]Revision, error) {
	if _, err := s.Get(ctx, bookID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT book_id,revision,storage_key,cover_key,cover_media_type,cover_width,cover_height,content_index_key,checksum,file_size,change_type,change_summary,is_original,created_at FROM book_revisions WHERE book_id=? ORDER BY created_at DESC, revision DESC`, bookID)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	var result []Revision
	for rows.Next() {
		entry, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) RestoreRevision(ctx context.Context, bookID string, input RestoreInput) (PublishResult, error) {
	entry, err := s.revision(ctx, bookID, input.Revision)
	if err != nil {
		return PublishResult{}, err
	}
	reader, err := s.store.Open(ctx, entry.StorageKey)
	if err != nil {
		return PublishResult{}, err
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return PublishResult{}, errors.Join(readErr, closeErr)
	}
	source := &sourceRevision{data: data, coverKey: entry.CoverKey, coverMediaType: entry.CoverMediaType, coverWidth: entry.CoverWidth, coverHeight: entry.CoverHeight}
	return s.publish(ctx, bookID, input.BaseRevision, "restore", input.Summary, source, func(*epubedit.Workspace) (*epubedit.CoverInfo, error) { return nil, nil })
}

func (s *Service) revision(ctx context.Context, bookID, revision string) (Revision, error) {
	row := s.db.QueryRowContext(ctx, `SELECT book_id,revision,storage_key,cover_key,cover_media_type,cover_width,cover_height,content_index_key,checksum,file_size,change_type,change_summary,is_original,created_at FROM book_revisions WHERE book_id=? AND revision=?`, bookID, revision)
	entry, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	return entry, err
}

type revisionScanner interface{ Scan(...any) error }

func scanRevision(row revisionScanner) (Revision, error) {
	var entry Revision
	var original int
	var created string
	err := row.Scan(&entry.BookID, &entry.Revision, &entry.StorageKey, &entry.CoverKey, &entry.CoverMediaType, &entry.CoverWidth, &entry.CoverHeight, &entry.ContentIndexKey, &entry.Checksum, &entry.FileSize, &entry.ChangeType, &entry.ChangeSummary, &original, &created)
	if err != nil {
		return Revision{}, err
	}
	entry.Original = original == 1
	entry.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Revision{}, err
	}
	return entry, nil
}

func (s *Service) readCurrent(ctx context.Context, bookID string) (Book, []byte, error) {
	book, reader, err := s.Open(ctx, bookID)
	if err != nil {
		return Book{}, nil, err
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return Book{}, nil, errors.Join(readErr, closeErr)
	}
	return book, data, nil
}
func mapContentError(err error) error {
	if err == nil {
		return nil
	}
	var invalidEPUB *epubedit.InvalidContentError
	if errors.As(err, &invalidEPUB) {
		return &InvalidContentError{Cause: err}
	}
	if errors.Is(err, epubedit.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
func nextRevision(current, timeNow time.Time) time.Time {
	candidate := timeNow.UTC()
	if !candidate.After(current) {
		candidate = current.Add(time.Nanosecond)
	}
	return candidate
}
func revisionStorageKey(book Book, revision string) string {
	safe := strings.NewReplacer(":", "-", ".", "-").Replace(revision)
	return path.Join("books", book.ID, "revisions", safe+"-"+newID("object"), book.Filename)
}
func coverStorageKey(bookID, revision, mediaType string) string {
	ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[mediaType]
	safe := strings.NewReplacer(":", "-", ".", "-").Replace(revision)
	return path.Join("books", bookID, "derived", safe+"-"+newID("cover")+"."+ext)
}
func (s *Service) currentRevision(ctx context.Context, bookID string) string {
	var value string
	_ = s.db.QueryRowContext(ctx, `SELECT content_revision FROM books WHERE id=?`, bookID).Scan(&value)
	return value
}
func (s *Service) deleteObjects(ctx context.Context, keys []string) {
	for _, key := range keys {
		if key != "" {
			_ = s.store.Delete(ctx, key)
		}
	}
}

func (s *Service) pruneRevisions(ctx context.Context, bookID, current string) {
	limit := 5
	if value, err := s.setting(ctx, "revision_retention"); err == nil && value != "" {
		if parsed, parseErr := strconv.Atoi(value); parseErr == nil && parsed >= 1 && parsed <= 100 {
			limit = parsed
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT revision,storage_key,cover_key FROM book_revisions WHERE book_id=? AND is_original=0 ORDER BY created_at DESC,revision DESC LIMIT -1 OFFSET ?`, bookID, limit)
	if err != nil {
		return
	}
	defer rows.Close()
	type candidate struct{ revision, key, cover string }
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if rows.Scan(&item.revision, &item.key, &item.cover) == nil {
			candidates = append(candidates, item)
		}
	}
	for _, item := range candidates {
		result, err := s.db.ExecContext(ctx, `DELETE FROM book_revisions WHERE book_id=? AND revision=? AND is_original=0 AND revision<>?`, bookID, item.revision, current)
		if err != nil {
			continue
		}
		affected, _ := result.RowsAffected()
		if affected == 1 {
			s.deleteObjects(ctx, []string{item.key, item.cover})
		}
	}
}
