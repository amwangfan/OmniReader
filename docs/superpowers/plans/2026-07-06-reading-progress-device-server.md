# Reading Progress and Device Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement device registration/management, independent per-device progress, derived global progress, daily reading totals, and the web Sync dashboard in the Go server.

**Architecture:** A new `internal/reading` service owns device, progress, and activity transactions. Focused HTTP handlers expose the contract and the existing Sync module renders service summaries. SQLite remains authoritative and locator JSON is validated before storage.

**Tech Stack:** Go 1.25, SQLite, `net/http`, `html/template`, existing auth/storage services

---

## Shared Contract

Server and Android use locator v1 with these camelCase fields: `version`, `contentRevision`, `chapterHref`, `chapterIndex`, `blockIndex`, `charOffset`, `textQuote`, `textHash`, `chapterProgress`, and `bookProgress`. Device fields are `id`, `displayName`, `systemName`, `platform`, `manufacturer`, `model`, `appVersion`, `lastSeenAt`, and `disabledAt`. Book responses add `contentRevision`.

### Task 1: Add the schema and book revision

**Files:**
- Modify: `server/internal/db/migrations.go`
- Modify: `server/internal/db/migrations_test.go`
- Modify: `server/internal/books/service.go`
- Modify: `server/internal/books/service_test.go`

- [ ] **Step 1: Write failing migration assertions**

Inspect `PRAGMA table_info` and assert `books.content_revision`, new device/progress columns, and `reading_daily` exist. Insert a legacy book before migration 3 and assert its revision parses with `time.RFC3339Nano`.

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/db -run TestMigrations
```

Expected: FAIL because migration 3 is absent.

- [ ] **Step 3: Add migration version 3**

```sql
ALTER TABLE books ADD COLUMN content_revision TEXT NOT NULL DEFAULT '';
UPDATE books SET content_revision = updated_at WHERE content_revision = '';
ALTER TABLE devices ADD COLUMN system_name TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN manufacturer TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN app_version TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN disabled_at TEXT;
ALTER TABLE reading_progress ADD COLUMN content_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE reading_progress ADD COLUMN client_updated_at TEXT;
CREATE TABLE reading_daily (
  book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  reading_date TEXT NOT NULL,
  read_seconds INTEGER NOT NULL CHECK (read_seconds >= 0),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (book_id, device_id, reading_date)
);
CREATE INDEX reading_progress_updated_idx ON reading_progress(updated_at DESC);
CREATE INDEX reading_daily_device_date_idx ON reading_daily(device_id, reading_date DESC);
```

- [ ] **Step 4: Extend book storage/query code**

Add `ContentRevision time.Time` with JSON name `contentRevision`. Create uses the service clock. Update all SELECT lists, scanners, upload/list tests, and Android-facing JSON fixtures.

- [ ] **Step 5: Verify GREEN and commit**

```bash
go test ./internal/db ./internal/books
git add server/internal/db server/internal/books
git commit -m "feat: add reading and content revision schema"
```

### Task 2: Implement device lifecycle service

**Files:**
- Create: `server/internal/reading/models.go`
- Create: `server/internal/reading/service.go`
- Create: `server/internal/reading/service_test.go`

- [ ] **Step 1: Write failing tests**

Cover registration, repeat registration preserving a custom display name, rename, list/detail, last-seen refresh, disable, and rejection of disabled writes.

```go
type DeviceInput struct {
	ID, DisplayName, SystemName, Platform, Manufacturer, Model, AppVersion string
}
func (s *Service) UpsertDevice(context.Context, DeviceInput) (Device, error)
func (s *Service) ListDevices(context.Context) ([]DeviceSummary, error)
func (s *Service) GetDevice(context.Context, string) (DeviceDetail, error)
func (s *Service) RenameDevice(context.Context, string, string) (Device, error)
func (s *Service) DisableDevice(context.Context, string) error
```

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/reading -run Device
```

- [ ] **Step 3: Implement normalized upsert and summaries**

Require a UUID-shaped ID; trim/bound strings; fall back from system name to manufacturer/model; never overwrite an existing non-empty display name during repeat registration. Summaries join latest book/progress and today, seven-day, and total seconds. Disabled records remain readable and return typed `ErrDeviceDisabled` on writes.

- [ ] **Step 4: Verify GREEN and commit**

```bash
go test ./internal/reading -run Device
git add server/internal/reading
git commit -m "feat: add device lifecycle service"
```

### Task 3: Implement locators, progress, and daily totals

**Files:**
- Modify: `server/internal/reading/models.go`
- Modify: `server/internal/reading/service.go`
- Modify: `server/internal/reading/service_test.go`

- [ ] **Step 1: Write failing tests**

```go
type Locator struct {
	Version int `json:"version"`
	ContentRevision string `json:"contentRevision"`
	ChapterHref string `json:"chapterHref"`
	ChapterIndex int `json:"chapterIndex"`
	BlockIndex int `json:"blockIndex"`
	CharOffset int `json:"charOffset"`
	TextQuote string `json:"textQuote"`
	TextHash string `json:"textHash"`
	ChapterProgress float64 `json:"chapterProgress"`
	BookProgress float64 `json:"bookProgress"`
}
type ProgressInput struct {
	BookID, DeviceID string
	Locator Locator
	Percentage *float64
	ClientUpdatedAt *time.Time
	DailyReadSeconds map[string]int64
}
```

Prove device A/B rows remain independent, global is the newest server update, reading the global result does not mutate rows, repeated daily totals are idempotent, larger totals replace smaller totals, and invalid input changes nothing.

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/reading -run 'Progress|Daily|Locator'
```

- [ ] **Step 3: Validate and store transactionally**

Accept only locator version 1; RFC 3339 revision; non-negative indexes; `[0,1]` progress; bounded strings; and a usable chapter href or book progress. Use server time for ordering. Upsert only `(book_id, device_id)`. Daily rows use `MAX(read_seconds, excluded.read_seconds)` in the same transaction.

```go
func (s *Service) PutProgress(context.Context, ProgressInput) (ProgressResult, error)
func (s *Service) GetProgress(ctx context.Context, bookID, deviceID string) (ProgressResult, error)
func (s *Service) DeviceActivity(ctx context.Context, deviceID, from, to string) (Activity, error)
```

The global query orders by `updated_at DESC, device_id`. Responses compare locator revision to `books.content_revision`.

- [ ] **Step 4: Verify GREEN and commit**

```bash
go test ./internal/reading
git add server/internal/reading
git commit -m "feat: store per-device reading progress and totals"
```

### Task 4: Add authenticated REST APIs

**Files:**
- Create: `server/internal/httpapi/reading.go`
- Create: `server/internal/httpapi/reading_test.go`
- Modify: `server/internal/httpapi/server.go`
- Modify: `server/cmd/omnireader-server/main.go`

- [ ] **Step 1: Write failing contract tests**

Cover all designed routes, auth rejection, camelCase JSON, missing resources, disabled writes, validation errors, and atomicity.

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/httpapi -run 'Device|Progress|Activity'
```

- [ ] **Step 3: Wire routes**

Extend `httpapi.Options` with `ReadingService *reading.Service`, instantiate it in `main.go`, and register:

```go
mux.HandleFunc("PUT /api/v1/devices/current", putCurrentDevice(...))
mux.HandleFunc("GET /api/v1/devices", listDevices(...))
mux.HandleFunc("GET /api/v1/devices/{deviceID}", getDevice(...))
mux.HandleFunc("PATCH /api/v1/devices/{deviceID}", patchDevice(...))
mux.HandleFunc("DELETE /api/v1/devices/{deviceID}", disableDevice(...))
mux.HandleFunc("GET /api/v1/books/{bookID}/progress", getBookProgress(...))
mux.HandleFunc("PUT /api/v1/books/{bookID}/progress", putBookProgress(...))
mux.HandleFunc("GET /api/v1/devices/{deviceID}/activity", getDeviceActivity(...))
```

- [ ] **Step 4: Decode and map errors safely**

Use `http.MaxBytesReader`, reject unknown fields, map typed errors to stable JSON error codes, and never expose SQL errors.

- [ ] **Step 5: Verify GREEN and commit**

```bash
go test ./internal/httpapi -run 'Device|Progress|Activity'
git add server/internal/httpapi server/cmd/omnireader-server/main.go
git commit -m "feat: expose reading and device APIs"
```

### Task 5: Implement the Sync dashboard

**Files:**
- Modify: `server/internal/httpapi/server.go`
- Modify: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing page tests**

Seed two devices, progress rows, and daily totals. Assert `/admin/sync` renders totals, device/system names, recent books, readable locator summaries, global source device, rename forms, detail links, disable controls, and one persistent admin shell.

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/httpapi -run Sync
```

- [ ] **Step 3: Render and mutate through the service**

Pass `ReadingService` into `syncPage`, format durations and chapter/block positions, add authenticated web POST rename/disable routes, and expose raw locator JSON only inside collapsed `<details>`.

- [ ] **Step 4: Verify GREEN and commit**

```bash
go test ./internal/httpapi -run Sync
git add server/internal/httpapi
git commit -m "feat: add device reading dashboard"
```

### Task 6: Verify and deploy

**Files:**
- Verify all files above

- [ ] **Step 1: Format, test, and build on N100**

```bash
gofmt -w internal/db internal/books internal/reading internal/httpapi cmd/omnireader-server
go test ./...
go build ./cmd/omnireader-server
```

- [ ] **Step 2: Run an authenticated two-device smoke test**

Register two UUIDs, upload a fixture EPUB, submit distinct locators/totals, verify global/per-device results, disable one device, assert its write returns `403`, and confirm its history remains readable.

- [ ] **Step 3: Deploy safely**

Test migration 3 against a copied demo database first. Deploy to `/tmp/omnireader-demo` while preserving config, books, and live data. Retain the previous binary until health and smoke checks pass.

- [ ] **Step 4: Inspect final state**

```bash
git diff --check
git status --short --branch
```

Expected: clean server feature branch and all tests passing.
