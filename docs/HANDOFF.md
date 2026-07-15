# OmniReader project handoff

Last updated: 2026-07-15

This document is the context-independent handoff for the server and Android repositories. A new maintainer or a new ChatGPT/Codex account should be able to continue from this file without access to the earlier conversation.

## 1. Repository state

| Area | Repository | Current work | Status |
|---|---|---|---|
| Server/API/web | [amwangfan/OmniReader](https://github.com/amwangfan/OmniReader) | `agent/server-sync-hardening`, [Draft PR #2](https://github.com/amwangfan/OmniReader/pull/2) | Automated server tests pass; remote deployment not verified |
| Android | [amwangfan/OmniReader_app](https://github.com/amwangfan/OmniReader_app) | `agent/android-sync-preview`, [Draft PR #1](https://github.com/amwangfan/OmniReader_app/pull/1) | Unit tests and Debug APK build pass; no emulator/device test |

Both PRs target `main`, are mergeable, and intentionally remain Draft. Do not assume the default branches contain the preview implementation.

Source-of-truth order for ongoing work:

1. the two active preview branches and their tests;
2. this handoff and each repository's `CHANGELOG.md`;
3. the Draft PR descriptions and check results;
4. the dated MVP design and implementation plan, which describe the original intent but contain some now-superseded repository paths and scope assumptions.

No credentials, Tailscale keys, SSH keys, private EPUB files or deployment secrets are stored in either repository.

## 2. Product state

The system is a usable vertical prototype, not a fully accepted MVP.

The implemented path is:

```text
web upload (EPUB/MOBI/AZW/AZW3/TXT/PDF/HTML)
    -> optional Calibre conversion
    -> validated EPUB + SQLite metadata
    -> authenticated Android download
    -> local simple EPUB reader
    -> local chapter progress
    -> foreground/background progress sync
    -> web device/activity display
```

The Android client remains EPUB-only. Multi-format support is deliberately implemented on the server as conversion, not as multiple Android rendering engines.

## 3. What was changed from the initial skeleton

### Server

- Added device upsert/list APIs and a synchronization service.
- Added per-device reading progress read/write APIs and timestamp-based last-write-wins behavior.
- Added database indexes and migration coverage for synchronization queries.
- Made the web synchronization page show devices and recent book progress instead of fixed placeholder counts.
- Added web refresh-cookie renewal and a logout action that revokes the current refresh session.
- Changed book removal to soft archiving so the database row and EPUB remain recoverable.
- Added real EPUB package validation rather than trusting the filename extension.
- Added title/author search and a single-book details endpoint.
- Added Calibre `ebook-convert` support for MOBI, AZW, AZW3, TXT, PDF, HTML and HTM.
- Persisted the original source format while continuing to expose EPUB downloads.
- Added a converter-status API, clear unavailable-converter errors, a 128 MB source limit, a 64 MB EPUB limit and a five-minute timeout.
- Added GitHub Actions for the Go test suite and a real Calibre TXT-to-EPUB smoke test.
- Added temporary Linux deployment guidance.

### Android

- Added automatic access-token refresh and a single retry after HTTP 401.
- Added server-side refresh-token revocation during logout.
- Made changing the configured server clear credentials from the previous server.
- Added a stable generated device ID and device registration.
- Added local chapter persistence, resume-on-open and bidirectional progress reconciliation.
- Added a shared, unit-tested last-write-wins sync policy for foreground and background work.
- Added WorkManager synchronization every six hours with a network constraint and retry handling.
- Changed downloads to `.part` files, SHA-256 validation and atomic replacement.
- Serialized local index updates and made index writes atomic.
- Disabled Android backup and aligned the Kotlin/JVM toolchain to Java 17.
- Added GitHub Actions for unit tests and Debug APK assembly.

See the repository changelogs for file-level summaries.

## 4. API contract used by Android

All application endpoints are authenticated except login/refresh and health checks.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/auth/login` | Obtain access and refresh tokens |
| `POST` | `/api/v1/auth/refresh` | Renew the access token |
| `POST` | `/api/v1/auth/logout` | Revoke a refresh session |
| `GET` | `/api/v1/books` | List/search available books |
| `GET` | `/api/v1/books/{bookId}` | Read one book's metadata |
| `GET` | `/api/v1/books/{bookId}/download` | Download the normalized EPUB |
| `GET/PUT` | `/api/v1/books/{bookId}/progress` | Read/write latest progress |
| `GET` | `/api/v1/devices` | List registered devices |
| `PUT` | `/api/v1/devices/current` | Register/update the current device |
| `GET` | `/api/v1/conversion` | Inspect converter availability |

Current progress locators are `chapter:N`. Conflict resolution compares device-supplied timestamps. This is intentionally simple and is not equivalent to a Readium locator, a server revision, or a within-chapter position.

## 5. Verification evidence and limits

### Passed

- Server: `go test ./...` on GitHub Actions.
- Server: real Calibre installed in CI; a TXT fixture converted to EPUB and the result passed EPUB metadata parsing.
- Android: `testDebugUnitTest` and `assembleDebug` on GitHub Actions.
- Both branches previously passed `git diff --check`.

Recorded runs:

- [Server run 29304256063](https://github.com/amwangfan/OmniReader/actions/runs/29304256063)
- [Android run 29303113280](https://github.com/amwangfan/OmniReader_app/actions/runs/29303113280)

### Not verified

- No emulator or physical Android device interaction test.
- No full upload-download-read-sync test between the two repositories.
- No live TXT/MOBI/PDF upload through a remotely deployed web UI.
- No deployment on the intended Aliyun host.
- No load, disk-full, interrupted-conversion or hostile-document test.
- No production backup/restore or upgrade/rollback rehearsal.

An earlier attempt to join the user's tailnet directly from a restricted sandbox failed before authentication because the sandbox lacked TUN/CAP_NET_ADMIN and blocked route Netlink access. No Tailscale node was created. Future remote work should use a self-hosted GitHub Actions runner on the server, a GitHub-hosted runner configured with user-owned secrets, or deployment commands executed by the user over Tailscale. Never place a Tailscale or SSH secret in chat or in the repository.

## 6. Known gaps and risks

### Required before the Draft PRs should be considered accepted

1. Deploy the server privately and run real web/API smoke tests with EPUB, TXT and at least one non-DRM MOBI or text-based PDF.
2. Run the Android app on an emulator or device against that server.
3. Verify login, download, checksum failure handling, restart/resume, manual sync, background sync, access-token expiry and two-device conflict behavior.
4. Verify that server-generated fields and Android serialization agree using an explicit shared JSON contract fixture.

### Data consistency work

- Foreground sync and WorkManager can run at the same time.
- Separate `LocalBookStore` instances do not share an in-memory mutex; add a process-wide coordinator and/or file lock before relying on concurrent sync.
- The progress model trusts device clocks. A clock set far into the future can dominate later updates. Prefer a server-assigned revision or server receive time.
- Server archive state is not clearly represented on the Android shelf.
- Local delete, re-download, repair and server-checksum update flows are not implemented.

### Reader limitations

- Chapter index only; no within-chapter scroll restoration.
- Minimal plain-text extraction rather than complete EPUB CSS, images, links, notes and layout support.
- No table of contents UI despite the original MVP design listing basic TOC as required.
- No themes, typography controls, bookmarks, highlights, notes or reading statistics.
- Consider Readium Locator compatibility before expanding the custom `chapter:N` format.

### Security and operations

- Android tokens are in app-private preferences, not Keystore-backed encrypted storage.
- Cleartext HTTP is enabled for private Tailscale testing.
- Public deployment still needs HTTPS, secure cookies, CSRF protection, login throttling and safer conversion sandboxing.
- Refresh-token rotation is not complete.
- There is no documented, tested backup/restore, systemd installer, upgrade/rollback path, release signing or monitoring.
- Calibre processes untrusted inputs and must run as an unprivileged account with resource limits.
- Archive restore/permanent purge is not implemented; the project owner explicitly deprioritized archive UI while conversion and core MVP work are being validated.

## 7. Recommended next-step options

| Option | Outcome | Why choose it now |
|---|---|---|
| A. Private server deployment | Running Aliyun/Tailscale test service plus real conversion smoke results | Best next step because server code is tested but not deployed |
| B. Android end-to-end test | Evidence that login/download/read/resume/sync works on a real runtime | Required before Android PR can leave Draft |
| C. Sync consistency hardening | Global sync coordinator, file locking and API contract fixtures | Reduces risk of local index/progress corruption |
| D. Security and operations | Keystore, HTTPS/CSRF/rate limits, backup and systemd lifecycle | Required before internet exposure or dependable use |
| E. Reader upgrade | TOC, within-chapter position, layout and Readium-compatible locator | Improves reading quality after the data path is stable |

Recommended order: A -> B -> C -> D -> E. If A cannot be authorized, do C while the owner prepares a private deployment path.

## 8. Private deployment acceptance checklist

1. Build the server from `agent/server-sync-hardening`.
2. Install Calibre and confirm `ebook-convert --version`.
3. Configure strong secrets outside Git and bind to loopback or a private Tailscale address.
4. Confirm `GET /healthz`.
5. Log in and confirm `GET /api/v1/conversion` reports available.
6. Upload EPUB and TXT, then one non-DRM MOBI or text-based PDF.
7. Download outputs and verify they are valid EPUB ZIPs with `META-INF/container.xml`.
8. Search by title/author and inspect the single-book endpoint.
9. Connect Android and exercise the end-to-end checklist above.
10. Record commands, non-sensitive results and failures in the PRs.

## 9. Instructions for a new account

1. Connect the GitHub integration with access to both repositories.
2. Read this file, both READMEs and both CHANGELOGs.
3. Inspect Draft PR #2 and Draft PR #1, including their latest head commits and checks.
4. Continue on the existing preview branches unless the repository owner explicitly requests a new branch.
5. Do not merge either PR or expose the service publicly until its stated acceptance checks pass.
6. Ask the owner only for deployment decisions or credentials that materially block progress; secrets must be supplied through GitHub Secrets or another protected channel, never pasted into chat.

Suggested continuation prompt:

```text
Continue the OmniReader project from the two existing Draft PRs:
- server: amwangfan/OmniReader PR #2, branch agent/server-sync-hardening
- Android: amwangfan/OmniReader_app PR #1, branch agent/android-sync-preview

First read docs/HANDOFF.md in the server branch, both READMEs and both CHANGELOGs, then verify the current PR heads and checks through GitHub. Treat the dated design/plan as historical intent and the active branches as source of truth. Do not merge to main yet. Clearly separate automated-test evidence from emulator/device and remote-deployment evidence. The recommended next goal is private server deployment and a real EPUB/TXT/MOBI-or-PDF smoke test, followed by Android end-to-end verification. Never request that secrets be pasted into chat; use protected repository secrets or user-executed Tailscale commands.
```
