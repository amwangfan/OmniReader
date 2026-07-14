# OmniReader server changes

## 2026-07-14

### Added

- Device registration and listing APIs:
  - `PUT /api/v1/devices/current`
  - `GET /api/v1/devices`
- Reading progress APIs:
  - `GET /api/v1/books/{bookId}/progress`
  - `PUT /api/v1/books/{bookId}/progress`
- A synchronization service with per-device progress, latest-progress lookup, timestamp conflict handling, validation, database indexes, and tests.
- Registered-device count on the web synchronization page.
- Automatic web access-cookie renewal through an HttpOnly refresh cookie.
- A GitHub Actions workflow for running the server test suite on test branches and pull requests.

### Changed

- `DELETE /api/v1/books/{bookId}` and the web library action now archive books instead of permanently deleting their database row and EPUB file.
- EPUB uploads are limited to 64 MB and must contain a readable EPUB package document rather than merely using an `.epub` filename.
- The main README now points to the standalone Android repository.

### Remaining

- There is not yet a permanent-purge or archive-restore interface.
- The web synchronization page shows the registered-device count but does not yet show detailed per-book progress.
- The web admin still needs an explicit logout action for revoking its refresh session.
- Deployment automation, CI, backup/restore, login rate limiting, and production HTTPS remain to be added.
