# Reading Progress and Device Management Design

## Goal

Add precise per-device reading progress, a global resume position, lightweight reading-time statistics, and web device management across the `OmniReader` server and the native Android `OmniReader_app`.

This is the first delivery. EPUB content editing is specified separately because content changes must use the locator and revision rules defined here.

## Repository Boundaries

- `E:\Codex\Projects\OmniReader`: SQLite migrations, Go domain services, authenticated REST APIs, and the web Sync module.
- `E:\Codex\Projects\OmniReader_app`: Android device identity, EPUB block parsing, local reading state, time accumulation, API calls, and reader resume behavior.

The server owns the API contract. Both repositories use feature branches, preserve existing local commits, and retain the repository owner's Git author identity.

## Chosen Architecture

SQLite is the primary data store. The progress locator is versioned JSON stored inside a structured progress row. Per-book sidecar JSON is not the authoritative store, but a future export operation may generate it for backup.

The system stores one latest progress row per device and book. It does not copy the global progress into every device row. The global position is a derived view: the per-device row with the latest server receipt time.

Reading time is stored as absolute daily totals reported by Android. The server does not require a heartbeat or retain every update event.

## Content Revision

Books gain a `content_revision` string rather than a numeric `content_version`. It uses an RFC 3339 UTC timestamp with sub-second precision, for example:

```text
2026-07-06T14:25:30.123Z
```

Initial migration assigns the migration execution time to existing books. New uploads receive their creation timestamp as the first revision. The web UI converts revision timestamps to the configured/local display timezone.

The progress locator carries the revision against which it was produced. A mismatch does not delete or rewrite the saved progress.

## Progress Locator v1

```json
{
  "version": 1,
  "contentRevision": "2026-07-06T14:25:30.123Z",
  "chapterHref": "OPS/chapter-03.xhtml",
  "chapterIndex": 2,
  "blockIndex": 17,
  "charOffset": 42,
  "textQuote": "Text surrounding the current position",
  "textHash": "sha256-of-normalized-block-text",
  "chapterProgress": 0.38,
  "bookProgress": 0.21
}
```

Reading blocks include paragraphs, headings, list items, block quotes, and preformatted blocks. The Android reader restores a locator in this order:

1. exact `chapterHref` and matching `textHash`;
2. matching `textHash` elsewhere in the chapter;
3. `chapterHref` plus `blockIndex` and `charOffset`;
4. `chapterIndex` plus `chapterProgress`;
5. `bookProgress` across the current spine;
6. start of the closest known chapter.

`textQuote` is bounded to a short context string and is used for diagnostics and a final fuzzy match. Locator input is size-limited and validated before storage.

## Database Changes

### Books

Add:

- `content_revision TEXT NOT NULL`.

### Devices

Keep the existing stable client-generated `id` and add:

- `system_name TEXT NOT NULL DEFAULT ''`;
- `manufacturer TEXT NOT NULL DEFAULT ''`;
- `model TEXT NOT NULL DEFAULT ''`;
- `app_version TEXT NOT NULL DEFAULT ''`;
- `disabled_at TEXT`.

`display_name` defaults to the Android system device name and remains editable in the web UI. A disabled device cannot upload new state but its progress and statistics remain queryable.

### Reading Progress

Keep the `(book_id, device_id)` primary key and store:

- validated locator JSON;
- book percentage;
- locator `content_revision`;
- optional client timestamp for display only;
- authoritative server `updated_at`.

Server receipt time determines the global latest position. Client clocks never determine conflict order.

### Daily Reading

Add `reading_daily` with:

- `book_id`;
- `device_id`;
- local calendar `reading_date` in `YYYY-MM-DD` form;
- `read_seconds` as the Android-reported absolute daily total;
- `updated_at`.

The primary key is `(book_id, device_id, reading_date)`. Repeated reports are idempotent: the stored value becomes the greater of the existing and reported totals. Negative values are rejected. This table intentionally contains no per-update event history.

## Device Identity

Android generates a random UUID on first launch and persists it in existing app preferences. It does not use IMEI, Android ID, serial number, or another hardware identifier.

The default device name is read from the Android system device-name setting when accessible. If unavailable, it falls back to `Build.MANUFACTURER + Build.MODEL`. Clearing app data creates a new UUID and therefore a new device entry.

Device registration occurs after login and before progress synchronization. Registration is an authenticated upsert and refreshes `last_seen_at`, system metadata, and app version without overwriting a user-edited display name.

## REST API

All endpoints remain under `/api/v1` and use the existing bearer authentication.

```text
PUT    /api/v1/devices/current
GET    /api/v1/devices
GET    /api/v1/devices/{deviceId}
PATCH  /api/v1/devices/{deviceId}
DELETE /api/v1/devices/{deviceId}

GET    /api/v1/books/{bookId}/progress?deviceId={deviceId}
PUT    /api/v1/books/{bookId}/progress
GET    /api/v1/devices/{deviceId}/activity?from=YYYY-MM-DD&to=YYYY-MM-DD
```

`PUT /devices/current` accepts the stable UUID, system/default name, platform, manufacturer, model, and app version. The response contains the server device record and whether it is disabled.

`GET /books/{bookId}/progress` returns:

- the requesting device's saved progress, if any;
- the derived global latest progress and its source device;
- the current book `contentRevision`;
- a `revisionMismatch` flag for each returned locator.

`PUT /books/{bookId}/progress` accepts:

- `deviceId`;
- locator v1;
- percentage;
- optional client update time;
- a bounded map of recent `dailyReadSeconds` keyed by local date.

It updates only the submitting device's row. It never modifies another device's progress. The response returns the saved device row and the newly derived global row.

`DELETE /devices/{deviceId}` sets `disabled_at`; it does not physically delete progress or daily statistics.

## Global and Per-Device Resume Semantics

- Every device row remains unchanged until that device submits actual new reading state.
- The global progress is the per-device row with the newest server `updated_at`.
- Android opens a book at the global position by default, even when it came from another device.
- Applying the global position locally does not immediately upload or overwrite the current device row.
- The current device row changes only after the reader scrolls, changes chapter, or accumulates reading seconds.
- The UI shows a short message when resuming from another device.

This preserves each device's last actual position while providing a predictable global continuation point.

## Android Reader Changes

`EpubParser` exposes source chapter hrefs and ordered reading blocks with normalized text hashes. `ReaderScreen` renders blocks in a `LazyColumn`, allowing the first visible block and its scroll offset to form a locator.

A separate `reading_state.json` stores:

- local/remote book identity;
- device UUID;
- latest local locator;
- daily absolute read-second totals;
- dirty/synced state;
- last accepted server update time.

Reading time is counted only while the reader screen is foreground and active. The app persists state when the visible block changes, chapters change, the reader closes, or the application enters the background. It attempts synchronization on login, manual sync, book close/background, and network recovery. Offline failures never block local reading.

On synchronization:

1. register/update the current device;
2. upload dirty local progress and daily totals;
3. fetch current per-device and global state;
4. adopt global state only for resume presentation when local state is not dirty;
5. mark uploads clean using the server response timestamp.

## Web Sync Module

The existing Sync page becomes a lightweight dashboard with:

- device count;
- today's total reading time;
- seven-day reading time;
- the global latest book, locator summary, source device, and update time;
- device cards showing display/system name, model, app version, last seen time, latest book, and latest locator;
- device detail with per-book latest progress plus daily and total reading seconds;
- rename and disable actions.

Locator summaries show chapter and block/paragraph rather than raw JSON. Raw JSON remains available in an expandable diagnostics section.

## Errors and Validation

- Unknown or archived books return `404` or a clear archived-state error.
- Unknown devices require registration before progress upload.
- Disabled devices receive `403` on writes but remain readable to admin endpoints.
- Malformed, oversized, or unsupported locator versions return `400` without changing existing progress.
- Daily maps have bounded entry counts and dates; invalid totals reject the request atomically.
- Database updates for progress and daily totals occur in one transaction.
- Android retains dirty local state after any network or server error and retries later.

## Testing and Acceptance

Server tests cover migrations, device upsert/rename/disable, independent per-device progress, last-received global selection, revision mismatch reporting, idempotent daily totals, validation, authentication, and rendered device dashboards.

Android tests cover UUID persistence, device-name fallback, block parsing/hashing, locator serialization and fallback restoration, foreground time accumulation, local state migration, dirty-state retry, and HTTP request/response contracts.

Acceptance requires two Android device identities alternating through the same fixture book:

1. device A reads and uploads;
2. device B resumes from A, reads further, and uploads;
3. the global row becomes B's row;
4. A's previous row remains unchanged;
5. A resumes from B but does not update A's row until actual reading occurs;
6. web totals and per-device positions match the submitted daily totals.

## Non-goals

- Multi-user authorization.
- Heartbeat validation or forensic reading-session logs.
- Annotation/highlight synchronization.
- Automatic rewriting of all saved progress after EPUB edits.
- Root-specific Android behavior.
