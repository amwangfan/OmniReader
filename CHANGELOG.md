# OmniReader server changes

## 2026-07-15

### Documentation and handoff

- Rewrote the README as an environment-independent project entry point.
- Removed stale machine-specific paths and deployment addresses from the setup flow.
- Added `docs/HANDOFF.md` with the two-repository state, completed modifications, test evidence, unverified areas, risks, deployment checklist, next-step options and a continuation prompt.
- Clarified that the original design and plan are historical baselines and that the active Draft PR branches are the implementation source of truth.
- No server runtime behavior changed in this documentation-only update.

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
- Detailed device activity and recent per-book reading progress on the web synchronization page.
- A web sign-out action that revokes the browser refresh session and clears both authentication cookies.
- Calibre-backed conversion of MOBI, AZW, AZW3, TXT, PDF, HTML, and HTM uploads into validated EPUB files.
- Persistent source-format metadata, an authenticated conversion-status API, single-book details, and title/author search.

### Changed

- `DELETE /api/v1/books/{bookId}` and the web library action now archive books instead of permanently deleting their database row and EPUB file.
- EPUB uploads are limited to 64 MB and must contain a readable EPUB package document rather than merely using an `.epub` filename.
- Source uploads are limited to 128 MB, conversion is limited to five minutes, and converted EPUB output remains limited to 64 MB.
- The main README now points to the standalone Android repository.

### Remaining

- There is not yet a permanent-purge or archive-restore interface.
- Calibre must be installed separately for non-EPUB conversion; scanned PDFs still require OCR before upload.
- Deployment automation, backup/restore, login rate limiting, and production HTTPS remain to be added.
