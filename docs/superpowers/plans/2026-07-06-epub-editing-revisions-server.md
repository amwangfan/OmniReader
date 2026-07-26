# EPUB Editing and Immutable Revisions Implementation Plan

**Goal:** Add authenticated EPUB metadata, cover, chapter source/spine editing, immutable revision history, restore, and a lightweight browser editor without risking the current downloadable book.

**Architecture:** A new `internal/epubedit` package owns safe EPUB extraction, inspection, mutation, validation, and deterministic rebuild. `internal/books` coordinates storage plus SQLite publication transactions. Every mutation compares `baseRevision`, writes a new immutable object, atomically advances the book row, and retains the original plus five newest generated revisions. HTTP handlers expose JSON/multipart endpoints; Novel Management mounts a persistent-shell editor panel with a sandboxed preview.

**Constraints:** Keep local filesystem storage behind the existing storage interface; reject zip-slip, scripts, event handlers, oversized archives/files, malformed XML/XHTML, and empty spines; preserve unknown OPF metadata; write `mimetype` first and uncompressed; never modify existing progress rows; use fixed-nine-digit UTC revisions.

---

## Task 1: Schema and storage model

- Add migration 4 for `book_revisions` and current-book cover metadata.
- Backfill each existing book as its immutable original revision.
- Extend storage keys so revisions and derived cover previews are separate immutable objects.
- Test migration idempotency, original uniqueness, foreign keys, and deletion behavior.

## Task 2: Safe EPUB workspace and inspection

- Add EPUB 2/3 fixtures and failing tests for zip-slip, entry-count/expanded-size limits, malformed container/OPF/XHTML, missing manifest/spine items, and executable content.
- Implement bounded extraction into a unique temporary directory with unconditional cleanup.
- Parse container, OPF, manifest, spine, navigation/title data, metadata, and cover using namespace-aware XML.
- Rebuild with `mimetype` first/uncompressed and re-open/re-validate the result.

## Task 3: Pure mutations

- Implement metadata update while preserving unknown nodes/namespaces.
- Implement JPEG/PNG/WebP cover replacement based on decoded bytes and bounded dimensions.
- Implement chapter XHTML read/update, add, delete, title update, and spine reorder using stable manifest-derived editor IDs.
- Reject invalid XHTML, new scripts/event handlers, escaping references, deletion of the final spine item, duplicate/unknown spine IDs, and stale chapter IDs.
- Test EPUB 2/3, nested paths, inline formatting, cover-present/absent, and navigation-title updates.

## Task 4: Publication and revision service

- Add content/revision domain DTOs and typed errors (`not found`, `conflict`, `invalid content`).
- Implement compare-and-publish: read current, verify base revision, mutate/rebuild, save immutable EPUB and cover preview, then transactionally insert revision and update `books` metadata/current key/checksum/size/revision.
- On database failure, best-effort delete newly saved objects; prune only after commit.
- Retain original plus five newest non-original revisions; restoring copies selected bytes into a new revision.
- Test stale base revisions, no-change-on-failure, concurrent writes, retention, restore-as-new, and unchanged reading progress.

## Task 5: REST API

- Add the designed `/api/v1/books/{bookId}/content`, metadata, cover, chapter, spine, revision-list, and restore routes.
- Require bearer authentication, reject unknown JSON fields, cap JSON/XHTML/multipart sizes, and map typed errors to 400/404/409/500 without leaking SQL or paths.
- Add end-to-end handler tests for every mutation and conflict response.

## Task 6: Lightweight Novel Management editor

- Add an Edit action and persistent-shell editor route/panel.
- Render metadata, cover preview, ordered chapters, XHTML textarea, sandboxed iframe preview, revision history, and restore controls.
- Keep navigation client-side; refresh `baseRevision` after each successful mutation; show field-scoped validation and 409 refresh guidance.
- Test one admin root/header after navigation, sandbox attributes, and no full-page refresh during module/editor transitions.

## Task 7: Verification and deployment

- Run `gofmt`, `go test ./...`, `go build ./cmd/omnireader-server`, and `git diff --check` on N100.
- Run mutation acceptance against a copied demo database and copied books directory.
- Deploy only after restore and invalid-XHTML rollback tests pass; preserve live data and retain the previous binary until health checks pass.
- Verify login redirect, authenticated editor load, metadata/chapter mutation, revision restore, and clean temporary workspace removal.

## Task 8: Android revision consumption follow-up

- Add update-available detection by comparing catalog and local `contentRevision`.
- Download replacement transactionally, parse before replacing, retain old file on failure, and resolve old locator against the new blocks.
- Build/test on N100, install on BKQ-AN10, and verify old-revision progress resumes after a server-side chapter edit.

