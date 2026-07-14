package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

var (
	ErrDeviceNotFound   = errors.New("device not found")
	ErrProgressNotFound = errors.New("reading progress not found")
)

type Options struct {
	Now func() time.Time
}

type Device struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Platform    string    `json:"platform"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Progress struct {
	BookID     string    `json:"bookId"`
	DeviceID   string    `json:"deviceId"`
	Locator    string    `json:"locator"`
	Percentage *float64  `json:"percentage,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ProgressActivity struct {
	Progress
	BookTitle  string `json:"bookTitle"`
	DeviceName string `json:"deviceName"`
}

type UpsertDeviceInput struct {
	ID          string
	DisplayName string
	Platform    string
}

type PutProgressInput struct {
	BookID     string
	DeviceID   string
	Locator    string
	Percentage *float64
	UpdatedAt  time.Time
}

func NewService(db *sql.DB, opts Options) (*Service, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: db, now: now}, nil
}

func (s *Service) UpsertDevice(ctx context.Context, input UpsertDeviceInput) (Device, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return Device{}, errors.New("device id is required")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = "Android device"
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = "android"
	}
	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO devices (id, display_name, platform, last_seen_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  display_name = excluded.display_name,
  platform = excluded.platform,
  last_seen_at = excluded.last_seen_at,
  updated_at = excluded.updated_at
`, id, displayName, platform, formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return Device{}, fmt.Errorf("upsert device: %w", err)
	}
	return s.GetDevice(ctx, id)
}

func (s *Service) GetDevice(ctx context.Context, id string) (Device, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, display_name, platform, last_seen_at, created_at, updated_at
FROM devices
WHERE id = ?
`, strings.TrimSpace(id))
	device, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrDeviceNotFound
	}
	return device, err
}

func (s *Service) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, display_name, platform, last_seen_at, created_at, updated_at
FROM devices
ORDER BY last_seen_at DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	devices := make([]Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}
	return devices, nil
}

func (s *Service) PutProgress(ctx context.Context, input PutProgressInput) (Progress, error) {
	bookID := strings.TrimSpace(input.BookID)
	deviceID := strings.TrimSpace(input.DeviceID)
	locator := strings.TrimSpace(input.Locator)
	if bookID == "" || deviceID == "" || locator == "" {
		return Progress{}, errors.New("book id, device id, and locator are required")
	}
	if input.Percentage != nil && (*input.Percentage < 0 || *input.Percentage > 1) {
		return Progress{}, errors.New("percentage must be between 0 and 1")
	}
	if err := s.requireExists(ctx, "books", bookID, "book not found"); err != nil {
		return Progress{}, err
	}
	if err := s.requireExists(ctx, "devices", deviceID, "device not found"); err != nil {
		return Progress{}, err
	}
	updatedAt := input.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = s.now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO reading_progress (book_id, device_id, locator, percentage, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(book_id, device_id) DO UPDATE SET
  locator = excluded.locator,
  percentage = excluded.percentage,
  updated_at = excluded.updated_at
WHERE julianday(excluded.updated_at) >= julianday(reading_progress.updated_at)
`, bookID, deviceID, locator, input.Percentage, formatTime(updatedAt))
	if err != nil {
		return Progress{}, fmt.Errorf("save reading progress: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return s.GetDeviceProgress(ctx, bookID, deviceID)
	}
	now := formatTime(s.now())
	if _, err := s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at = ?, updated_at = ? WHERE id = ?`, now, now, deviceID); err != nil {
		return Progress{}, fmt.Errorf("update device last seen: %w", err)
	}
	return s.GetDeviceProgress(ctx, bookID, deviceID)
}

func (s *Service) GetLatestProgress(ctx context.Context, bookID string) (Progress, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT book_id, device_id, locator, percentage, updated_at
FROM reading_progress
WHERE book_id = ?
ORDER BY julianday(updated_at) DESC
LIMIT 1
`, strings.TrimSpace(bookID))
	progress, err := scanProgress(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, ErrProgressNotFound
	}
	return progress, err
}

func (s *Service) GetDeviceProgress(ctx context.Context, bookID string, deviceID string) (Progress, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT book_id, device_id, locator, percentage, updated_at
FROM reading_progress
WHERE book_id = ? AND device_id = ?
`, strings.TrimSpace(bookID), strings.TrimSpace(deviceID))
	progress, err := scanProgress(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, ErrProgressNotFound
	}
	return progress, err
}

func (s *Service) ListRecentProgress(ctx context.Context, limit int) ([]ProgressActivity, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT rp.book_id, rp.device_id, rp.locator, rp.percentage, rp.updated_at,
       books.title, devices.display_name
FROM reading_progress AS rp
JOIN books ON books.id = rp.book_id
JOIN devices ON devices.id = rp.device_id
ORDER BY julianday(rp.updated_at) DESC, rp.book_id, rp.device_id
LIMIT ?
`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent progress: %w", err)
	}
	defer rows.Close()

	activities := make([]ProgressActivity, 0)
	for rows.Next() {
		var activity ProgressActivity
		var percentage sql.NullFloat64
		var updatedAt string
		if err := rows.Scan(
			&activity.BookID, &activity.DeviceID, &activity.Locator, &percentage, &updatedAt,
			&activity.BookTitle, &activity.DeviceName,
		); err != nil {
			return nil, fmt.Errorf("scan recent progress: %w", err)
		}
		if percentage.Valid {
			activity.Percentage = &percentage.Float64
		}
		activity.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent progress: %w", err)
	}
	return activities, nil
}

func (s *Service) requireExists(ctx context.Context, table string, id string, message string) error {
	query := "SELECT 1 FROM " + table + " WHERE id = ?"
	var exists int
	err := s.db.QueryRowContext(ctx, query, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New(message)
	}
	if err != nil {
		return fmt.Errorf("check %s: %w", table, err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(row scanner) (Device, error) {
	var device Device
	var lastSeenAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(&device.ID, &device.DisplayName, &device.Platform, &lastSeenAt, &createdAt, &updatedAt); err != nil {
		return Device{}, err
	}
	var err error
	if device.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return Device{}, err
	}
	if device.CreatedAt, err = parseTime(createdAt); err != nil {
		return Device{}, err
	}
	if device.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Device{}, err
	}
	return device, nil
}

func scanProgress(row scanner) (Progress, error) {
	var progress Progress
	var percentage sql.NullFloat64
	var updatedAt string
	if err := row.Scan(&progress.BookID, &progress.DeviceID, &progress.Locator, &percentage, &updatedAt); err != nil {
		return Progress{}, err
	}
	if percentage.Valid {
		progress.Percentage = &percentage.Float64
	}
	parsed, err := parseTime(updatedAt)
	if err != nil {
		return Progress{}, err
	}
	progress.UpdatedAt = parsed
	return progress, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time: %w", err)
	}
	return parsed, nil
}
