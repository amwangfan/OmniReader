package reading

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

const fixedUTCTimeLayout = "2006-01-02T15:04:05.000000000Z"

var (
	ErrNotFound       = errors.New("not found")
	ErrDeviceDisabled = errors.New("device disabled")
	ErrValidation     = errors.New("validation failed")
)

type Options struct {
	Now func() time.Time
}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(database *sql.DB, opts Options) (*Service, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: database, now: now}, nil
}

func (s *Service) UpsertDevice(ctx context.Context, input DeviceInput) (Device, error) {
	input = normalizeDeviceInput(input)
	if err := validateDeviceInput(input); err != nil {
		return Device{}, err
	}
	now := formatTime(s.now())
	result, err := s.db.ExecContext(ctx, `
INSERT INTO devices (id, display_name, system_name, platform, manufacturer, model, app_version, last_seen_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  system_name = excluded.system_name,
  platform = excluded.platform,
  manufacturer = excluded.manufacturer,
  model = excluded.model,
  app_version = excluded.app_version,
  last_seen_at = excluded.last_seen_at,
  updated_at = excluded.updated_at
WHERE devices.disabled_at IS NULL
`, input.ID, input.DisplayName, input.SystemName, input.Platform, input.Manufacturer, input.Model, input.AppVersion, now, now, now)
	if err != nil {
		return Device{}, fmt.Errorf("upsert device: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Device{}, fmt.Errorf("inspect device upsert: %w", err)
	}
	if changed == 0 {
		return Device{}, ErrDeviceDisabled
	}
	return s.device(ctx, input.ID)
}

func (s *Service) RenameDevice(ctx context.Context, id, displayName string) (Device, error) {
	if err := validateUUID(id); err != nil {
		return Device{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 128 {
		return Device{}, validation("displayName is required and must not exceed 128 bytes")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET display_name = ?, updated_at = ? WHERE id = ? AND disabled_at IS NULL`, displayName, formatTime(s.now()), id)
	if err != nil {
		return Device{}, fmt.Errorf("rename device: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		device, readErr := s.device(ctx, id)
		if readErr != nil {
			return Device{}, readErr
		}
		if device.DisabledAt != nil {
			return Device{}, ErrDeviceDisabled
		}
		return Device{}, ErrNotFound
	}
	return s.device(ctx, id)
}

func (s *Service) DisableDevice(ctx context.Context, id string) error {
	if err := validateUUID(id); err != nil {
		return err
	}
	now := formatTime(s.now())
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET disabled_at = COALESCE(disabled_at, ?), updated_at = ? WHERE id = ?`, now, now, id)
	if err != nil {
		return fmt.Errorf("disable device: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ListDevices(ctx context.Context) ([]DeviceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM devices ORDER BY last_seen_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan device id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate device ids: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]DeviceSummary, 0, len(ids))
	for _, id := range ids {
		detail, err := s.GetDevice(ctx, id)
		if err != nil {
			return nil, err
		}
		var latest *BookProgressSummary
		if len(detail.Books) > 0 {
			copy := detail.Books[0]
			latest = &copy
		}
		result = append(result, DeviceSummary{Device: detail.Device, LatestBook: latest, TodayReadSeconds: detail.TodayReadSeconds, SevenDayReadSeconds: detail.SevenDayReadSeconds, TotalReadSeconds: detail.TotalReadSeconds})
	}
	return result, nil
}

func (s *Service) GetDevice(ctx context.Context, id string) (DeviceDetail, error) {
	device, err := s.device(ctx, id)
	if err != nil {
		return DeviceDetail{}, err
	}
	books, err := s.deviceBooks(ctx, id)
	if err != nil {
		return DeviceDetail{}, err
	}
	today := s.now().UTC().Format("2006-01-02")
	sevenStart := s.now().UTC().AddDate(0, 0, -6).Format("2006-01-02")
	var todaySeconds, sevenSeconds, totalSeconds int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN reading_date = ? THEN read_seconds END),0), COALESCE(SUM(CASE WHEN reading_date BETWEEN ? AND ? THEN read_seconds END),0), COALESCE(SUM(read_seconds),0) FROM reading_daily WHERE device_id = ?`, today, sevenStart, today, id).Scan(&todaySeconds, &sevenSeconds, &totalSeconds); err != nil {
		return DeviceDetail{}, fmt.Errorf("read device totals: %w", err)
	}
	return DeviceDetail{Device: device, Books: books, TodayReadSeconds: todaySeconds, SevenDayReadSeconds: sevenSeconds, TotalReadSeconds: totalSeconds}, nil
}

func (s *Service) PutProgress(ctx context.Context, input ProgressInput) (ProgressResult, error) {
	if err := validateProgressInput(input, s.now()); err != nil {
		return ProgressResult{}, err
	}
	parsedRevision, _ := time.Parse(time.RFC3339Nano, input.Locator.ContentRevision)
	input.Locator.ContentRevision = parsedRevision.UTC().Format(fixedUTCTimeLayout)
	locatorJSON, err := json.Marshal(input.Locator)
	if err != nil {
		return ProgressResult{}, validation("locator cannot be encoded")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProgressResult{}, fmt.Errorf("begin progress transaction: %w", err)
	}
	defer tx.Rollback()
	var disabledAt sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT disabled_at FROM devices WHERE id = ?`, input.DeviceID).Scan(&disabledAt); errors.Is(err, sql.ErrNoRows) {
		return ProgressResult{}, ErrNotFound
	} else if err != nil {
		return ProgressResult{}, fmt.Errorf("read progress device: %w", err)
	} else if disabledAt.Valid {
		return ProgressResult{}, ErrDeviceDisabled
	}
	var currentRevision string
	if err := tx.QueryRowContext(ctx, `SELECT content_revision FROM books WHERE id = ? AND archived_at IS NULL`, input.BookID).Scan(&currentRevision); errors.Is(err, sql.ErrNoRows) {
		return ProgressResult{}, ErrNotFound
	} else if err != nil {
		return ProgressResult{}, fmt.Errorf("read progress book: %w", err)
	}
	now := formatTime(s.now())
	var clientUpdatedAt any
	if input.ClientUpdatedAt != nil {
		clientUpdatedAt = formatTime(*input.ClientUpdatedAt)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO reading_progress (book_id, device_id, locator, percentage, content_revision, client_updated_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(book_id, device_id) DO UPDATE SET locator=excluded.locator, percentage=excluded.percentage, content_revision=excluded.content_revision, client_updated_at=excluded.client_updated_at, updated_at=excluded.updated_at
`, input.BookID, input.DeviceID, string(locatorJSON), input.Percentage, input.Locator.ContentRevision, clientUpdatedAt, now)
	if err != nil {
		return ProgressResult{}, fmt.Errorf("save progress: %w", err)
	}
	for date, seconds := range input.DailyReadSeconds {
		if _, err := tx.ExecContext(ctx, `INSERT INTO reading_daily (book_id, device_id, reading_date, read_seconds, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(book_id, device_id, reading_date) DO UPDATE SET read_seconds = MAX(reading_daily.read_seconds, excluded.read_seconds), updated_at = excluded.updated_at`, input.BookID, input.DeviceID, date, seconds, now); err != nil {
			return ProgressResult{}, fmt.Errorf("save daily reading: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ProgressResult{}, fmt.Errorf("commit progress: %w", err)
	}
	return s.GetProgress(ctx, input.BookID, input.DeviceID)
}

func (s *Service) GetProgress(ctx context.Context, bookID, deviceID string) (ProgressResult, error) {
	var revision string
	if err := s.db.QueryRowContext(ctx, `SELECT content_revision FROM books WHERE id = ? AND archived_at IS NULL`, bookID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return ProgressResult{}, ErrNotFound
	} else if err != nil {
		return ProgressResult{}, fmt.Errorf("read book revision: %w", err)
	}
	result := ProgressResult{ContentRevision: revision}
	if deviceID != "" {
		if _, err := s.device(ctx, deviceID); err != nil {
			return ProgressResult{}, err
		}
		progress, err := s.progress(ctx, `WHERE rp.book_id = ? AND rp.device_id = ?`, bookID, deviceID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ProgressResult{}, err
		}
		if err == nil {
			result.Device = &progress
		}
	}
	global, err := s.progress(ctx, `WHERE rp.book_id = ? ORDER BY rp.updated_at DESC, rp.device_id LIMIT 1`, bookID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ProgressResult{}, err
	}
	if err == nil {
		result.Global = &global
	}
	return result, nil
}

func (s *Service) DeviceActivity(ctx context.Context, deviceID, from, to string) (Activity, error) {
	if _, err := s.device(ctx, deviceID); err != nil {
		return Activity{}, err
	}
	today := s.now().UTC()
	if from == "" {
		from = today.AddDate(0, 0, -29).Format("2006-01-02")
	}
	if to == "" {
		to = today.Format("2006-01-02")
	}
	fromTime, err := parseDate(from)
	if err != nil {
		return Activity{}, err
	}
	toTime, err := parseDate(to)
	if err != nil {
		return Activity{}, err
	}
	if toTime.Before(fromTime) || toTime.Sub(fromTime) > 366*24*time.Hour {
		return Activity{}, validation("activity date range is invalid or exceeds 366 days")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT reading_date, SUM(read_seconds) FROM reading_daily WHERE device_id = ? AND reading_date BETWEEN ? AND ? GROUP BY reading_date ORDER BY reading_date`, deviceID, from, to)
	if err != nil {
		return Activity{}, fmt.Errorf("read activity: %w", err)
	}
	defer rows.Close()
	activity := Activity{DeviceID: deviceID, From: from, To: to, Days: []DailyActivity{}}
	for rows.Next() {
		var day DailyActivity
		if err := rows.Scan(&day.ReadingDate, &day.ReadSeconds); err != nil {
			return Activity{}, fmt.Errorf("scan activity: %w", err)
		}
		activity.Days = append(activity.Days, day)
		activity.TotalReadSeconds += day.ReadSeconds
	}
	if err := rows.Err(); err != nil {
		return Activity{}, fmt.Errorf("iterate activity: %w", err)
	}
	return activity, nil
}

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	devices, err := s.ListDevices(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	result := Dashboard{Devices: devices, DeviceCount: len(devices)}
	for _, device := range devices {
		result.TodayReadSeconds += device.TodayReadSeconds
		result.SevenDayReadSeconds += device.SevenDayReadSeconds
		result.TotalReadSeconds += device.TotalReadSeconds
	}
	var title string
	progress, err := s.progress(ctx, `ORDER BY rp.updated_at DESC, rp.device_id LIMIT 1`)
	if err == nil {
		result.Global = &progress
		if err := s.db.QueryRowContext(ctx, `SELECT title FROM books WHERE id = ?`, progress.BookID).Scan(&title); err != nil {
			return Dashboard{}, fmt.Errorf("read global book title: %w", err)
		}
		result.GlobalBookTitle = title
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Dashboard{}, err
	}
	return result, nil
}

func (s *Service) device(ctx context.Context, id string) (Device, error) {
	var result Device
	var lastSeen, disabled sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, display_name, system_name, platform, manufacturer, model, app_version, last_seen_at, disabled_at FROM devices WHERE id = ?`, id).Scan(&result.ID, &result.DisplayName, &result.SystemName, &result.Platform, &result.Manufacturer, &result.Model, &result.AppVersion, &lastSeen, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("read device: %w", err)
	}
	result.LastSeenAt, err = parseTime(lastSeen.String)
	if err != nil {
		return Device{}, err
	}
	if disabled.Valid {
		parsed, err := parseTime(disabled.String)
		if err != nil {
			return Device{}, err
		}
		result.DisabledAt = &parsed
	}
	return result, nil
}

func (s *Service) deviceBooks(ctx context.Context, id string) ([]BookProgressSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT rp.book_id, b.title, rp.locator, rp.percentage, rp.client_updated_at, rp.updated_at, b.content_revision,
       COALESCE((SELECT SUM(rd.read_seconds) FROM reading_daily rd WHERE rd.book_id=rp.book_id AND rd.device_id=rp.device_id),0)
FROM reading_progress rp JOIN books b ON b.id=rp.book_id
WHERE rp.device_id=? ORDER BY rp.updated_at DESC, rp.book_id`, id)
	if err != nil {
		return nil, fmt.Errorf("read device books: %w", err)
	}
	defer rows.Close()
	result := []BookProgressSummary{}
	for rows.Next() {
		var summary BookProgressSummary
		var locatorJSON, updatedAt, revision string
		var percentage sql.NullFloat64
		var clientUpdated sql.NullString
		if err := rows.Scan(&summary.BookID, &summary.Title, &locatorJSON, &percentage, &clientUpdated, &updatedAt, &revision, &summary.ReadSeconds); err != nil {
			return nil, fmt.Errorf("scan device book: %w", err)
		}
		if err := json.Unmarshal([]byte(locatorJSON), &summary.Locator); err != nil {
			return nil, fmt.Errorf("decode locator: %w", err)
		}
		if percentage.Valid {
			value := percentage.Float64
			summary.Percentage = &value
		}
		summary.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		if clientUpdated.Valid {
			value, err := parseTime(clientUpdated.String)
			if err != nil {
				return nil, err
			}
			summary.ClientUpdatedAt = &value
		}
		summary.RevisionMismatch = summary.Locator.ContentRevision != revision
		result = append(result, summary)
	}
	return result, rows.Err()
}

func (s *Service) progress(ctx context.Context, clause string, args ...any) (Progress, error) {
	query := `SELECT rp.book_id, rp.device_id, d.display_name, rp.locator, rp.percentage, rp.client_updated_at, rp.updated_at, b.content_revision FROM reading_progress rp JOIN devices d ON d.id=rp.device_id JOIN books b ON b.id=rp.book_id ` + clause
	return scanProgress(s.db.QueryRowContext(ctx, query, args...))
}

func scanProgress(row *sql.Row) (Progress, error) {
	var result Progress
	var locatorJSON, updatedAt, currentRevision string
	var percentage sql.NullFloat64
	var clientUpdated sql.NullString
	if err := row.Scan(&result.BookID, &result.DeviceID, &result.DeviceName, &locatorJSON, &percentage, &clientUpdated, &updatedAt, &currentRevision); err != nil {
		return Progress{}, err
	}
	if err := json.Unmarshal([]byte(locatorJSON), &result.Locator); err != nil {
		return Progress{}, fmt.Errorf("decode locator: %w", err)
	}
	if percentage.Valid {
		value := percentage.Float64
		result.Percentage = &value
	}
	var err error
	result.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Progress{}, err
	}
	if clientUpdated.Valid {
		value, err := parseTime(clientUpdated.String)
		if err != nil {
			return Progress{}, err
		}
		result.ClientUpdatedAt = &value
	}
	result.RevisionMismatch = result.Locator.ContentRevision != currentRevision
	return result, nil
}

func normalizeDeviceInput(input DeviceInput) DeviceInput {
	input.ID = strings.ToLower(strings.TrimSpace(input.ID))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.SystemName = strings.TrimSpace(input.SystemName)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Manufacturer = strings.TrimSpace(input.Manufacturer)
	input.Model = strings.TrimSpace(input.Model)
	input.AppVersion = strings.TrimSpace(input.AppVersion)
	if input.DisplayName == "" {
		input.DisplayName = input.SystemName
	}
	if input.DisplayName == "" {
		input.DisplayName = strings.TrimSpace(strings.Join([]string{input.Manufacturer, input.Model}, " "))
	}
	if input.DisplayName == "" {
		input.DisplayName = "Unknown device"
	}
	return input
}

func validateDeviceInput(input DeviceInput) error {
	if err := validateUUID(input.ID); err != nil {
		return err
	}
	for name, value := range map[string]string{"displayName": input.DisplayName, "systemName": input.SystemName, "platform": input.Platform, "manufacturer": input.Manufacturer, "model": input.Model, "appVersion": input.AppVersion} {
		if len(value) > 128 {
			return validation(name + " must not exceed 128 bytes")
		}
	}
	return nil
}

func validateProgressInput(input ProgressInput, now time.Time) error {
	if strings.TrimSpace(input.BookID) == "" {
		return validation("bookId is required")
	}
	if err := validateUUID(input.DeviceID); err != nil {
		return err
	}
	l := input.Locator
	if l.Version != 1 {
		return validation("locator version must be 1")
	}
	if _, err := time.Parse(time.RFC3339Nano, l.ContentRevision); err != nil {
		return validation("locator contentRevision must be RFC3339")
	}
	timeSeparator := strings.IndexByte(l.ContentRevision, 'T')
	if timeSeparator < 0 || strings.IndexAny(l.ContentRevision[timeSeparator+1:], ".,") < 0 {
		return validation("locator contentRevision must include a fractional second component")
	}
	if l.ChapterIndex < 0 || l.BlockIndex < 0 || l.CharOffset < 0 {
		return validation("locator indexes must be nonnegative")
	}
	if !unitInterval(l.ChapterProgress) || !unitInterval(l.BookProgress) {
		return validation("locator progress must be between 0 and 1")
	}
	if len(l.ChapterHref) > 2048 || len(l.TextQuote) > 512 || len(l.TextHash) > 128 {
		return validation("locator strings exceed limits")
	}
	if strings.TrimSpace(l.ChapterHref) == "" && l.BookProgress == 0 {
		return validation("locator requires chapterHref or bookProgress")
	}
	if input.Percentage != nil && !unitInterval(*input.Percentage) {
		return validation("percentage must be between 0 and 1")
	}
	if len(input.DailyReadSeconds) > 31 {
		return validation("dailyReadSeconds must not exceed 31 entries")
	}
	for date, seconds := range input.DailyReadSeconds {
		parsedDate, err := parseDate(date)
		if err != nil {
			return err
		}
		today, _ := time.Parse("2006-01-02", now.UTC().Format("2006-01-02"))
		if parsedDate.Before(today.AddDate(0, 0, -90)) || parsedDate.After(today.AddDate(0, 0, 1)) {
			return validation("daily reading date must be within the allowed recent window")
		}
		if seconds < 0 || seconds > 86400 {
			return validation("daily read seconds must be between 0 and 86400")
		}
	}
	return nil
}

func validateUUID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != strings.ToLower(value) {
		return validation("device id must be a canonical UUID")
	}
	return nil
}

func unitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, validation("date must use YYYY-MM-DD")
	}
	return parsed, nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp: %w", err)
	}
	return parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(fixedUTCTimeLayout)
}

func validation(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, message)
}
