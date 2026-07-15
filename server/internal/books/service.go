package books

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/amwangfan/omnireader/server/internal/storage"
)

type Service struct {
	db        *sql.DB
	store     storage.Store
	converter Converter
	now       func() time.Time
}

type Options struct {
	Now       func() time.Time
	Converter Converter
}

type Book struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Author     string     `json:"author"`
	Filename   string     `json:"filename"`
	Format     string     `json:"format"`
	SourceFormat string   `json:"sourceFormat"`
	StorageKey string     `json:"-"`
	FileSize   int64      `json:"fileSize"`
	Checksum   string     `json:"checksum"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type CreateInput struct {
	Filename string
	Title    string
	Author   string
	Body     io.Reader
}

type UpdateInput struct {
	Title    string
	Author   string
	Filename string
}

const (
	MaxEPUBSize   = 64 << 20
	MaxUploadSize = 128 << 20
)

func NewService(db *sql.DB, store storage.Store, opts Options) (*Service, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	if store == nil {
		return nil, errors.New("storage is required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	converter := opts.Converter
	if converter == nil {
		converter = NewCalibreConverter("ebook-convert")
	}
	return &Service{db: db, store: store, converter: converter, now: now}, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Book, error) {
	if input.Body == nil {
		return Book{}, errors.New("book body is required")
	}
	inputFormat := sourceFormat(input.Filename)
	if !isSupportedSourceFormat(inputFormat) {
		return Book{}, fmt.Errorf("unsupported book format %q; supported formats: %s", inputFormat, strings.Join(supportedSourceFormats, ", "))
	}

	data, err := io.ReadAll(io.LimitReader(input.Body, MaxUploadSize+1))
	if err != nil {
		return Book{}, fmt.Errorf("read book body: %w", err)
	}
	if len(data) == 0 {
		return Book{}, errors.New("book body is empty")
	}
	if len(data) > MaxUploadSize {
		return Book{}, errors.New("source file exceeds 128 MB limit")
	}
	if inputFormat != "epub" {
		conversionContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		data, err = s.converter.Convert(conversionContext, input.Filename, data)
		if err != nil {
			return Book{}, err
		}
	} else if len(data) > MaxEPUBSize {
		return Book{}, errors.New("epub file exceeds 64 MB limit")
	}

	title := strings.TrimSpace(input.Title)
	author := strings.TrimSpace(input.Author)
	metadata, metadataErr := ParseEPUBMetadata(data)
	if metadataErr != nil {
		return Book{}, fmt.Errorf("invalid epub: %w", metadataErr)
	}
	if title == "" {
		title = strings.TrimSpace(metadata.Title)
	}
	if author == "" {
		author = strings.TrimSpace(metadata.Author)
	}
	now := s.now()
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(input.Filename), filepath.Ext(input.Filename))
	}
	if title == "" {
		title = "Untitled"
	}

	template, err := s.FilenameTemplate(ctx)
	if err != nil {
		return Book{}, err
	}
	id := newID("book")
	storageKey := "books/" + id + "/" + RenderFilenameTemplate(template, title, author, now)
	if err := s.store.Save(ctx, storageKey, bytes.NewReader(data)); err != nil {
		return Book{}, err
	}

	book := Book{
		ID:         id,
		Title:      title,
		Author:     author,
		Filename:   path.Base(storageKey),
		Format:     "epub",
		SourceFormat: inputFormat,
		StorageKey: storageKey,
		FileSize:   int64(len(data)),
		Checksum:   checksum(data),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO books (id, title, author, format, source_format, storage_key, file_size, checksum, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, book.ID, book.Title, book.Author, book.Format, book.SourceFormat, book.StorageKey, book.FileSize, book.Checksum, formatTime(book.CreatedAt), formatTime(book.UpdatedAt))
	if err != nil {
		_ = s.store.Delete(ctx, storageKey)
		return Book{}, fmt.Errorf("insert book: %w", err)
	}
	return book, nil
}

func (s *Service) List(ctx context.Context) ([]Book, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, author, format, source_format, storage_key, file_size, checksum, archived_at, created_at, updated_at
FROM books
WHERE archived_at IS NULL
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	var result []Book
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, book)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}
	return result, nil
}

func (s *Service) Search(ctx context.Context, query string) ([]Book, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return s.List(ctx)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, author, format, source_format, storage_key, file_size, checksum, archived_at, created_at, updated_at
FROM books
WHERE archived_at IS NULL AND (instr(lower(title), lower(?)) > 0 OR instr(lower(author), lower(?)) > 0)
ORDER BY created_at DESC
`, query, query)
	if err != nil {
		return nil, fmt.Errorf("search books: %w", err)
	}
	defer rows.Close()
	result := make([]Book, 0)
	for rows.Next() {
		book, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, book)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate searched books: %w", err)
	}
	return result, nil
}

func (s *Service) ConversionStatus() ConversionStatus {
	return s.converter.Status()
}

func (s *Service) Get(ctx context.Context, id string) (Book, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, title, author, format, source_format, storage_key, file_size, checksum, archived_at, created_at, updated_at
FROM books
WHERE id = ? AND archived_at IS NULL
`, id)
	book, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, errors.New("book not found")
	}
	return book, err
}

func (s *Service) Open(ctx context.Context, id string) (Book, io.ReadCloser, error) {
	book, err := s.Get(ctx, id)
	if err != nil {
		return Book{}, nil, err
	}
	reader, err := s.store.Open(ctx, book.StorageKey)
	if err != nil {
		return Book{}, nil, err
	}
	return book, reader, nil
}

func (s *Service) Archive(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE books
SET archived_at = ?, updated_at = ?
WHERE id = ? AND archived_at IS NULL
`, formatTime(s.now()), formatTime(s.now()), id)
	if err != nil {
		return fmt.Errorf("archive book: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read archive result: %w", err)
	}
	if affected == 0 {
		return errors.New("book not found")
	}
	return nil
}

func (s *Service) UpdateDetails(ctx context.Context, id string, input UpdateInput) (Book, error) {
	book, err := s.Get(ctx, id)
	if err != nil {
		return Book{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Book{}, errors.New("title is required")
	}
	author := strings.TrimSpace(input.Author)
	filename := normalizeEPUBFilename(input.Filename)
	if filename == "" {
		filename = RenderFilenameTemplate(DefaultFilenameTemplate, title, author, s.now())
	}
	newStorageKey := path.Dir(book.StorageKey) + "/" + filename
	if newStorageKey != book.StorageKey {
		if err := s.store.Rename(ctx, book.StorageKey, newStorageKey); err != nil {
			return Book{}, err
		}
	}
	now := s.now()
	_, err = s.db.ExecContext(ctx, `
UPDATE books
SET title = ?, author = ?, storage_key = ?, updated_at = ?
WHERE id = ? AND archived_at IS NULL
`, title, author, newStorageKey, formatTime(now), id)
	if err != nil {
		if newStorageKey != book.StorageKey {
			_ = s.store.Rename(ctx, newStorageKey, book.StorageKey)
		}
		return Book{}, fmt.Errorf("update book details: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	book, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete book transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM reading_progress WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("delete reading progress: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM books WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete book row: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete book: %w", err)
	}
	if err := s.store.Delete(ctx, book.StorageKey); err != nil {
		return err
	}
	return nil
}

func (s *Service) FilenameTemplate(ctx context.Context) (string, error) {
	value, err := s.setting(ctx, "filename_template")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return DefaultFilenameTemplate, nil
	}
	return value, nil
}

func (s *Service) SetFilenameTemplate(ctx context.Context, pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = DefaultFilenameTemplate
	}
	return s.setSetting(ctx, "filename_template", pattern)
}

func (s *Service) setting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read setting %s: %w", key, err)
	}
	return value, nil
}

func (s *Service) setSetting(ctx context.Context, key string, value string) error {
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO settings (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, key, value, now)
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBook(row scanner) (Book, error) {
	var book Book
	var archivedAt sql.NullString
	var createdAt string
	var updatedAt string
	err := row.Scan(&book.ID, &book.Title, &book.Author, &book.Format, &book.SourceFormat, &book.StorageKey, &book.FileSize, &book.Checksum, &archivedAt, &createdAt, &updatedAt)
	if err != nil {
		return Book{}, err
	}
	if archivedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, archivedAt.String)
		if err != nil {
			return Book{}, fmt.Errorf("parse archived_at: %w", err)
		}
		book.ArchivedAt = &parsed
	}
	var errParse error
	book.CreatedAt, errParse = time.Parse(time.RFC3339Nano, createdAt)
	if errParse != nil {
		return Book{}, fmt.Errorf("parse created_at: %w", errParse)
	}
	book.UpdatedAt, errParse = time.Parse(time.RFC3339Nano, updatedAt)
	if errParse != nil {
		return Book{}, fmt.Errorf("parse updated_at: %w", errParse)
	}
	book.Filename = path.Base(book.StorageKey)
	return book, nil
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func newID(prefix string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func normalizeEPUBFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	filename = filepath.Base(filename)
	filename = invalidFilenameChars.ReplaceAllString(filename, "_")
	filename = strings.Join(strings.Fields(filename), " ")
	filename = strings.Trim(filename, " .-_")
	if filename == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".epub") {
		filename += ".epub"
	}
	return filename
}
