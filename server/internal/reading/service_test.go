package reading

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/amwangfan/omnireader/server/internal/db"
	_ "modernc.org/sqlite"
)

const (
	deviceA = "11111111-1111-4111-8111-111111111111"
	deviceB = "22222222-2222-4222-8222-222222222222"
)

func TestDeviceLifecyclePreservesRenamedDisplayName(t *testing.T) {
	service, clock := testReadingService(t)
	ctx := context.Background()
	registered, err := service.UpsertDevice(ctx, DeviceInput{ID: deviceA, SystemName: "Leaf 5", Platform: "android", Manufacturer: "Onyx", Model: "Leaf5", AppVersion: "1.0"})
	if err != nil {
		t.Fatalf("UpsertDevice returned error: %v", err)
	}
	if registered.DisplayName != "Leaf 5" || registered.SystemName != "Leaf 5" {
		t.Fatalf("unexpected registered device: %#v", registered)
	}
	renamed, err := service.RenameDevice(ctx, deviceA, "Bedside Reader")
	if err != nil || renamed.DisplayName != "Bedside Reader" {
		t.Fatalf("RenameDevice = %#v, %v", renamed, err)
	}
	clock.Advance(time.Minute)
	refreshed, err := service.UpsertDevice(ctx, DeviceInput{ID: deviceA, SystemName: "Leaf Five", Platform: "android", Manufacturer: "ONYX", Model: "Leaf 5C", AppVersion: "2.0"})
	if err != nil {
		t.Fatalf("repeat UpsertDevice returned error: %v", err)
	}
	if refreshed.DisplayName != "Bedside Reader" || refreshed.SystemName != "Leaf Five" || refreshed.AppVersion != "2.0" {
		t.Fatalf("repeat registration overwrote rename or missed metadata: %#v", refreshed)
	}
	if !refreshed.LastSeenAt.Equal(clock.Now()) {
		t.Fatalf("LastSeenAt = %v, want %v", refreshed.LastSeenAt, clock.Now())
	}
	listed, err := service.ListDevices(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != deviceA {
		t.Fatalf("ListDevices = %#v, %v", listed, err)
	}
	detail, err := service.GetDevice(ctx, deviceA)
	if err != nil || detail.ID != deviceA {
		t.Fatalf("GetDevice = %#v, %v", detail, err)
	}
}

func TestDeviceFallbackDisableAndDisabledWrites(t *testing.T) {
	service, _ := testReadingService(t)
	ctx := context.Background()
	device, err := service.UpsertDevice(ctx, DeviceInput{ID: deviceA, Manufacturer: "Onyx", Model: "Leaf5", Platform: "android"})
	if err != nil || device.DisplayName != "Onyx Leaf5" {
		t.Fatalf("fallback device = %#v, %v", device, err)
	}
	if err := service.DisableDevice(ctx, deviceA); err != nil {
		t.Fatalf("DisableDevice returned error: %v", err)
	}
	detail, err := service.GetDevice(ctx, deviceA)
	if err != nil || detail.DisabledAt == nil {
		t.Fatalf("disabled device should remain readable: %#v, %v", detail, err)
	}
	if _, err := service.UpsertDevice(ctx, DeviceInput{ID: deviceA, SystemName: "Again"}); !errors.Is(err, ErrDeviceDisabled) {
		t.Fatalf("disabled upsert error = %v, want ErrDeviceDisabled", err)
	}
	if _, err := service.RenameDevice(ctx, deviceA, "Again"); !errors.Is(err, ErrDeviceDisabled) {
		t.Fatalf("disabled rename error = %v, want ErrDeviceDisabled", err)
	}
}

func TestDisabledDeviceConditionalUpsertCannotRefresh(t *testing.T) {
	service, clock := testReadingService(t)
	ctx := context.Background()
	registered, err := service.UpsertDevice(ctx, DeviceInput{ID: deviceA, SystemName: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisableDevice(ctx, deviceA); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	if _, err := service.UpsertDevice(ctx, DeviceInput{ID: deviceA, SystemName: "Changed"}); !errors.Is(err, ErrDeviceDisabled) {
		t.Fatalf("conditional upsert error = %v", err)
	}
	stored, err := service.GetDevice(ctx, deviceA)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SystemName != "Original" || !stored.LastSeenAt.Equal(registered.LastSeenAt) {
		t.Fatalf("disabled row was refreshed: %#v", stored)
	}
}

func TestProgressRemainsIndependentAndGlobalUsesServerTime(t *testing.T) {
	service, clock := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	first := validProgress(deviceA)
	futureClient := clock.Now().Add(365 * 24 * time.Hour)
	first.ClientUpdatedAt = &futureClient
	first.DailyReadSeconds = map[string]int64{"2026-07-06": 60}
	resultA, err := service.PutProgress(ctx, first)
	if err != nil {
		t.Fatalf("PutProgress A: %v", err)
	}
	clock.Advance(time.Second)
	second := validProgress(deviceB)
	pastClient := clock.Now().Add(-365 * 24 * time.Hour)
	second.ClientUpdatedAt = &pastClient
	second.Locator.BlockIndex = 9
	second.Locator.BookProgress = .7
	resultB, err := service.PutProgress(ctx, second)
	if err != nil {
		t.Fatalf("PutProgress B: %v", err)
	}
	if resultB.Global == nil || resultB.Global.DeviceID != deviceB {
		t.Fatalf("global should be B: %#v", resultB.Global)
	}
	readA, err := service.GetProgress(ctx, "book-1", deviceA)
	if err != nil {
		t.Fatalf("GetProgress A: %v", err)
	}
	if readA.Device == nil || readA.Device.Locator.BlockIndex != resultA.Device.Locator.BlockIndex {
		t.Fatalf("A row changed: %#v", readA.Device)
	}
	if readA.Global == nil || readA.Global.DeviceID != deviceB {
		t.Fatalf("read global = %#v", readA.Global)
	}
	readAAgain, _ := service.GetProgress(ctx, "book-1", deviceA)
	if !readAAgain.Device.UpdatedAt.Equal(readA.Device.UpdatedAt) {
		t.Fatal("reading global mutated device row")
	}
}

func TestLatestQueriesOrderFractionalServerTimesChronologically(t *testing.T) {
	service, clock := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	clock.value = time.Date(2026, 7, 6, 12, 0, 0, 120000000, time.UTC)
	if _, err := service.PutProgress(ctx, validProgress(deviceA)); err != nil {
		t.Fatal(err)
	}
	clock.value = time.Date(2026, 7, 6, 12, 0, 0, 123000000, time.UTC)
	newer := validProgress(deviceB)
	newer.Locator.BlockIndex = 12
	if _, err := service.PutProgress(ctx, newer); err != nil {
		t.Fatal(err)
	}
	result, err := service.GetProgress(ctx, "book-1", deviceA)
	if err != nil {
		t.Fatal(err)
	}
	if result.Global == nil || result.Global.DeviceID != deviceB || result.Global.Locator.BlockIndex != 12 {
		t.Fatalf("global latest = %#v, want fractional-time-newer device B", result.Global)
	}
	var stored []string
	rows, err := service.db.Query(`SELECT updated_at FROM reading_progress ORDER BY updated_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		stored = append(stored, value)
	}
	if len(stored) != 2 || stored[0] != "2026-07-06T12:00:00.120000000Z" || stored[1] != "2026-07-06T12:00:00.123000000Z" {
		t.Fatalf("stored timestamps = %#v", stored)
	}
}

func TestDeviceLatestBookOrdersFractionalServerTimesChronologically(t *testing.T) {
	service, clock := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	revision := "2026-07-06T00:00:00.123456789Z"
	if _, err := service.db.Exec(`INSERT INTO books (id,title,format,storage_key,file_size,checksum,content_revision,created_at,updated_at) VALUES ('book-2','Second Book','epub','book2.epub',1,'sum',?,?,?)`, revision, revision, revision); err != nil {
		t.Fatal(err)
	}
	clock.value = time.Date(2026, 7, 6, 12, 0, 0, 120000000, time.UTC)
	if _, err := service.PutProgress(ctx, validProgress(deviceA)); err != nil {
		t.Fatal(err)
	}
	clock.value = time.Date(2026, 7, 6, 12, 0, 0, 123000000, time.UTC)
	input := validProgress(deviceA)
	input.BookID = "book-2"
	if _, err := service.PutProgress(ctx, input); err != nil {
		t.Fatal(err)
	}
	devices, err := service.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var summary *DeviceSummary
	for i := range devices {
		if devices[i].ID == deviceA {
			summary = &devices[i]
		}
	}
	if summary == nil || summary.LatestBook == nil || summary.LatestBook.BookID != "book-2" {
		t.Fatalf("device latest = %#v, want book-2", summary)
	}
}

func TestDisabledDeviceCannotPutProgress(t *testing.T) {
	service, _ := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	if err := service.DisableDevice(ctx, deviceA); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutProgress(ctx, validProgress(deviceA)); !errors.Is(err, ErrDeviceDisabled) {
		t.Fatalf("PutProgress error = %v, want ErrDeviceDisabled", err)
	}
}

func TestDailyTotalsAreMaxIdempotentAndSummarized(t *testing.T) {
	service, _ := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	input := validProgress(deviceA)
	input.DailyReadSeconds = map[string]int64{"2026-07-05": 120, "2026-07-06": 60}
	if _, err := service.PutProgress(ctx, input); err != nil {
		t.Fatalf("PutProgress: %v", err)
	}
	input.DailyReadSeconds = map[string]int64{"2026-07-05": 90, "2026-07-06": 180}
	if _, err := service.PutProgress(ctx, input); err != nil {
		t.Fatalf("repeat PutProgress: %v", err)
	}
	activity, err := service.DeviceActivity(ctx, deviceA, "2026-07-05", "2026-07-06")
	if err != nil {
		t.Fatalf("DeviceActivity: %v", err)
	}
	if activity.TotalReadSeconds != 300 || len(activity.Days) != 2 || activity.Days[0].ReadSeconds != 120 || activity.Days[1].ReadSeconds != 180 {
		t.Fatalf("unexpected activity: %#v", activity)
	}
	devices, err := service.ListDevices(ctx)
	if err != nil || devices[0].TodayReadSeconds != 180 || devices[0].SevenDayReadSeconds != 300 || devices[0].TotalReadSeconds != 300 || devices[0].LatestBook == nil {
		t.Fatalf("unexpected device summary: %#v, %v", devices, err)
	}
}

func TestInvalidProgressIsAtomic(t *testing.T) {
	service, _ := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	input := validProgress(deviceA)
	input.Locator.Version = 2
	input.DailyReadSeconds = map[string]int64{"2026-07-06": 100}
	if _, err := service.PutProgress(ctx, input); !errors.Is(err, ErrValidation) {
		t.Fatalf("PutProgress error = %v, want validation", err)
	}
	result, err := service.GetProgress(ctx, "book-1", deviceA)
	if err != nil || result.Device != nil || result.Global != nil {
		t.Fatalf("invalid input changed progress: %#v, %v", result, err)
	}
	activity, err := service.DeviceActivity(ctx, deviceA, "2026-07-06", "2026-07-06")
	if err != nil || activity.TotalReadSeconds != 0 {
		t.Fatalf("invalid input changed daily totals: %#v, %v", activity, err)
	}
}

func TestProgressRevisionMismatchAndValidation(t *testing.T) {
	service, _ := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	input := validProgress(deviceA)
	input.Locator.ContentRevision = "2026-07-01T00:00:00.000000001Z"
	result, err := service.PutProgress(ctx, input)
	if err != nil || result.Device == nil || !result.Device.RevisionMismatch {
		t.Fatalf("revision mismatch = %#v, %v", result, err)
	}
	bad := []ProgressInput{validProgress(deviceA), validProgress(deviceA), validProgress(deviceA), validProgress(deviceA)}
	bad[0].Locator.ChapterIndex = -1
	bad[1].Locator.BookProgress = 1.01
	bad[2].Locator.ContentRevision = "not-a-time"
	bad[3].DailyReadSeconds = map[string]int64{"2026-07-06": -1}
	for i, candidate := range bad {
		if _, err := service.PutProgress(ctx, candidate); !errors.Is(err, ErrValidation) {
			t.Errorf("invalid case %d error = %v", i, err)
		}
	}
}

func TestLocatorRequiresFractionalSyntaxButAllowsZeroNanoseconds(t *testing.T) {
	service, _ := testReadingService(t)
	registerDevices(t, service)
	input := validProgress(deviceA)
	input.Locator.ContentRevision = "2026-07-06T00:00:00Z"
	if _, err := service.PutProgress(context.Background(), input); !errors.Is(err, ErrValidation) {
		t.Fatalf("PutProgress error = %v, want validation", err)
	}
	input.Locator.ContentRevision = "2026-07-06T00:00:00.000Z"
	result, err := service.PutProgress(context.Background(), input)
	if err != nil {
		t.Fatalf("fractional whole-second revision rejected: %v", err)
	}
	if result.Device == nil || result.Device.Locator.ContentRevision != "2026-07-06T00:00:00.000000000Z" {
		t.Fatalf("canonical revision = %#v", result.Device)
	}
}

func TestDashboardPropagatesDatabaseErrors(t *testing.T) {
	service, _ := testReadingService(t)
	if err := service.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Dashboard(context.Background()); err == nil {
		t.Fatal("Dashboard should propagate database errors")
	}
}

func TestDailyDateWindowAndRevisionCanonicalization(t *testing.T) {
	service, _ := testReadingService(t)
	ctx := context.Background()
	registerDevices(t, service)
	accepted := validProgress(deviceA)
	accepted.Locator.ContentRevision = "2026-07-06T08:00:00.123456789+08:00"
	accepted.DailyReadSeconds = map[string]int64{"2026-04-07": 1, "2026-07-07": 2}
	result, err := service.PutProgress(ctx, accepted)
	if err != nil {
		t.Fatalf("boundary PutProgress: %v", err)
	}
	if result.Device.Locator.ContentRevision != "2026-07-06T00:00:00.123456789Z" || result.Device.RevisionMismatch {
		t.Fatalf("revision not canonicalized: %#v", result.Device)
	}
	for _, date := range []string{"2026-04-06", "2026-07-08"} {
		candidate := validProgress(deviceA)
		candidate.DailyReadSeconds = map[string]int64{date: 1}
		if _, err := service.PutProgress(ctx, candidate); !errors.Is(err, ErrValidation) {
			t.Errorf("date %s error = %v, want validation", date, err)
		}
	}
}

type testClock struct{ value time.Time }

func (c *testClock) Now() time.Time          { return c.value }
func (c *testClock) Advance(d time.Duration) { c.value = c.value.Add(d) }

func testReadingService(t *testing.T) (*Service, *testClock) {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.RunMigrations(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	revision := "2026-07-06T00:00:00.123456789Z"
	if _, err := conn.Exec(`INSERT INTO books (id,title,format,storage_key,file_size,checksum,content_revision,created_at,updated_at) VALUES ('book-1','Test Book','epub','book.epub',1,'sum',?,?,?)`, revision, revision, revision); err != nil {
		t.Fatalf("insert book: %v", err)
	}
	clock := &testClock{value: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	service, err := NewService(conn, Options{Now: clock.Now})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, clock
}

func registerDevices(t *testing.T, service *Service) {
	t.Helper()
	for _, input := range []DeviceInput{{ID: deviceA, SystemName: "A", Platform: "android"}, {ID: deviceB, SystemName: "B", Platform: "android"}} {
		if _, err := service.UpsertDevice(context.Background(), input); err != nil {
			t.Fatalf("register device: %v", err)
		}
	}
}

func validProgress(deviceID string) ProgressInput {
	percentage := .25
	return ProgressInput{
		BookID: "book-1", DeviceID: deviceID, Percentage: &percentage,
		Locator: Locator{Version: 1, ContentRevision: "2026-07-06T00:00:00.123456789Z", ChapterHref: "OPS/chapter-1.xhtml", ChapterIndex: 0, BlockIndex: 3, CharOffset: 4, TextQuote: "context", TextHash: "abc", ChapterProgress: .5, BookProgress: .25},
	}
}
