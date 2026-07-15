# OmniReader

OmniReader is an experimental single-user, self-hosted ebook library and reading-progress synchronization system. It consists of a Go/SQLite server with a server-rendered admin UI and a separate native Android client.

> Development status (2026-07-15): the latest server work is in [Draft PR #2](https://github.com/amwangfan/OmniReader/pull/2) on `agent/server-sync-hardening`. The `main` branch does not yet contain these changes. See [the handoff document](docs/HANDOFF.md) before continuing or merging.

## Repository map

| Component | Repository | Active preview |
|---|---|---|
| Server, API, web admin and design docs | This repository | [PR #2](https://github.com/amwangfan/OmniReader/pull/2) |
| Android reader | [OmniReader_app](https://github.com/amwangfan/OmniReader_app) | [PR #1](https://github.com/amwangfan/OmniReader_app/pull/1) |

The original [MVP design](docs/design/mvp-design.md) and [implementation plan](docs/plans/mvp-implementation-plan.md) are dated planning baselines. The current implementation status is documented in [CHANGELOG.md](CHANGELOG.md), [docs/HANDOFF.md](docs/HANDOFF.md), and the two Draft PRs.

## Implemented on the preview branch

- Password-protected web admin and authenticated REST API.
- Short-lived access tokens, refresh sessions, token refresh, logout and web cookie renewal.
- EPUB upload, metadata extraction, search, details and authenticated download.
- Calibre-backed conversion of MOBI, AZW, AZW3, TXT, PDF, HTML and HTM into validated EPUB.
- SQLite metadata, local-file storage, checksums and recoverable server-side archiving.
- Device registration and per-device reading-progress synchronization.
- Web views for registered devices and recent reading activity.
- Upload limits, conversion timeout, EPUB structure validation and automated tests.

Android always downloads EPUB. The server does not add native MOBI/PDF reading support to the app; it converts supported source files before storage.

## Local development

Requirements:

- Go 1.25 or newer.
- Calibre `ebook-convert` for non-EPUB uploads. EPUB-only operation works without Calibre.

From the repository root:

```bash
cd server
go test ./...

export OMNIREADER_ADMIN_PASSWORD='replace-with-a-strong-password'
export OMNIREADER_TOKEN_SECRET='replace-with-at-least-32-random-bytes'
go run ./cmd/omnireader-server
```

The default address is `127.0.0.1:8080` and the default data directory is `server/data` when the process is started from `server/`.

Configuration:

| Variable | Default | Purpose |
|---|---|---|
| `OMNIREADER_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `OMNIREADER_DATA_DIR` | `data` | Base runtime data directory |
| `OMNIREADER_BOOKS_DIR` | `<data>/books` | Stored EPUB directory |
| `OMNIREADER_DATABASE_PATH` | `<data>/app.db` | SQLite database path |
| `OMNIREADER_ADMIN_USERNAME` | `admin` | Bootstrap/admin username |
| `OMNIREADER_ADMIN_PASSWORD` | none | Required administrator password |
| `OMNIREADER_TOKEN_SECRET` | none | Required token-signing secret |
| `OMNIREADER_EBOOK_CONVERT` | `ebook-convert` | Calibre executable path |

Do not commit credentials, runtime data, private books or deployment-specific configuration.

## Verification status

The preview branch has passed:

- GitHub Actions `go test ./...`;
- a CI smoke test that installs real Calibre, converts TXT to EPUB and parses the generated EPUB;
- source-level and HTTP tests for authentication, storage, books, conversion, devices and progress.

Latest recorded server run: [GitHub Actions run 29304256063](https://github.com/amwangfan/OmniReader/actions/runs/29304256063).

The following have **not** been completed:

- temporary deployment on the intended Aliyun/Tailscale host;
- browser upload tests against a running remote service for real MOBI/PDF fixtures;
- Android-to-server end-to-end testing;
- production hardening, backup/restore and release operations.

Keep PR #2 as Draft until the live server smoke test is complete.

## Format limitations

- DRM-protected MOBI/AZW files are not supported.
- Scanned PDFs require OCR before upload.
- Complex, multi-column or image-heavy PDFs may convert poorly.
- Source uploads are limited to 128 MB, converted EPUB output to 64 MB, and a conversion to five minutes.

## Deployment and security

See [temporary Linux deployment](docs/deployment.md). The current preview should be exposed only over a trusted private network such as Tailscale or behind correctly configured HTTPS. Do not expose the admin UI directly to the public internet in its current state.

## Project handoff

[docs/HANDOFF.md](docs/HANDOFF.md) records the exact repository state, completed modifications, validation evidence, known risks, next-step options and a continuation prompt for a new account or maintainer.

## Authorship and AI-assisted development

The project and commits belong to the repository owner's GitHub identity. Codex has been used as an engineering assistant for design, implementation, testing and documentation; it is not presented as the code author.
