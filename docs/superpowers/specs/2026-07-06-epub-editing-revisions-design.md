# EPUB Editing and Revisions Design

## Goal

Allow the single admin user to replace covers, edit OPF metadata, and modify the internal chapter structure and XHTML of EPUB books without corrupting the current downloadable book or silently invalidating reading progress.

This is the second delivery and depends on the `content_revision` and locator semantics defined in the reading-progress specification.

## Scope

The first editor supports:

- cover preview and replacement;
- title, author, language, publisher, description, and other selected OPF metadata;
- chapter list and spine order;
- chapter creation, deletion, title changes, and reordering;
- XHTML source editing with a sandboxed preview;
- immutable revision history and restore.

A full rich-text/WYSIWYG editor is intentionally deferred.

## Revision Identity

Every published content mutation creates a new RFC 3339 UTC `content_revision` timestamp with sub-second precision. The revision timestamp is data, not a numeric sequence. Storage paths use a filesystem-safe representation of the same instant.

Every write request includes `baseRevision`. If it does not match the current book revision, the server returns `409 Conflict` and makes no changes. This prevents two browser tabs, a plugin, or another client from overwriting one another.

## Immutable Revision Storage

Add `book_revisions` with:

- revision ID/timestamp;
- book ID;
- immutable storage key;
- checksum;
- file size;
- change type;
- human-readable change summary;
- original-upload flag;
- created timestamp.

The original uploaded EPUB is retained permanently. By default, the server retains the original plus the five newest non-original revisions. The retention count is configurable in Settings. Pruning never deletes the current or original revision.

Restoring an old revision creates a new immutable revision containing the selected bytes. It does not move the current pointer backward or rewrite history.

The `books` row continues to reference the current storage key, checksum, size, cover key, and `content_revision`.

## Safe Mutation Pipeline

Each mutation follows one service-owned pipeline:

1. read the current EPUB through the storage interface;
2. verify `baseRevision`;
3. extract into a unique temporary working directory with zip-slip protection and size/count limits;
4. locate `container.xml`, OPF, manifest, spine, navigation documents, cover, and chapter resources;
5. apply one requested mutation;
6. validate XML/XHTML, references, manifest/spine integrity, cover resource, and required EPUB files;
7. rebuild the archive with uncompressed `mimetype` as the first entry;
8. parse the rebuilt EPUB again and calculate checksum, file size, cover preview, and content index;
9. save the immutable revision through the storage interface;
10. atomically insert revision metadata and update the current `books` row;
11. prune eligible old revisions only after commit.

Any failure before commit leaves the current EPUB unchanged. A failure after saving but before database commit removes the unreferenced new object on a best-effort basis and logs it for cleanup.

## XHTML Editing

The editor exposes the complete XHTML source for one chapter. The preview uses a sandboxed iframe without script execution, top navigation, forms, or same-origin privileges.

Before publishing, the server requires parseable XML/XHTML and rejects path changes or references that escape the book root. Scripts and event-handler attributes may remain in the stored source only when they already existed, but the web preview never executes them. Newly introduced executable content is rejected in the first version.

Chapter identity is based on a stable server editor ID mapped to its manifest item and href. Renaming a visible chapter title does not require changing its href. New chapters receive collision-safe hrefs and manifest IDs.

Deletion is rejected when it would leave the spine empty. The editor identifies inbound links to a chapter and warns before deletion; the first version does not automatically rewrite arbitrary cross-chapter links.

## Cover Replacement

Accepted uploads are JPEG, PNG, or WebP within configured byte and pixel limits. The server determines media type from decoded content rather than filename.

Replacement updates or creates:

- the image resource;
- OPF manifest media type and properties;
- EPUB 2 cover metadata when present;
- the server `cover_key` preview.

The original image may remain in old immutable revisions but is removed from the new EPUB when no manifest or content reference needs it.

## Metadata Editing

Metadata writes update the OPF package document while preserving unknown metadata and namespaces. Supported first-version fields are title, creator/author, language, publisher, description, identifier, and publication/modification date where present.

The current `books.title` and `books.author` mirror the published OPF values in the same transaction. Existing filename-template behavior does not automatically rename the stored current revision during metadata editing; filename changes remain an explicit Novel Management action.

## Content Index and Progress Compatibility

Publishing generates a compact content index for the new revision containing ordered spine hrefs, chapter titles, reading blocks, normalized text hashes, and coarse book percentages. It may be stored as a derived JSON object beside the immutable EPUB and can always be regenerated.

Existing `reading_progress` rows are not overwritten. They retain their old `contentRevision`. Android detects the mismatch after downloading the new revision and restores in this order: chapter href/text hash, text hash within chapter, block index, chapter progress, book progress, chapter start.

The web device view labels older locators as belonging to an earlier revision until a device publishes progress from the current revision.

## REST API

```text
GET    /api/v1/books/{bookId}/content
GET    /api/v1/books/{bookId}/chapters/{chapterId}
PUT    /api/v1/books/{bookId}/metadata
PUT    /api/v1/books/{bookId}/cover
PUT    /api/v1/books/{bookId}/chapters/{chapterId}
POST   /api/v1/books/{bookId}/chapters
PUT    /api/v1/books/{bookId}/spine
DELETE /api/v1/books/{bookId}/chapters/{chapterId}
GET    /api/v1/books/{bookId}/revisions
POST   /api/v1/books/{bookId}/revisions/{revision}/restore
```

Mutation requests include `baseRevision` and an optional change summary. Responses return the new book metadata, `contentRevision`, and revision entry.

Large XHTML bodies and cover uploads use explicit request-size limits. All endpoints use existing authentication.

## Web Novel Management

The Novel Management list adds an Edit action leading to a book editor with:

- current cover, metadata, revision, checksum, and file size;
- cover upload and preview;
- OPF metadata form;
- ordered chapter/spine list with add, delete, and reorder controls;
- XHTML textarea and sandboxed live preview;
- validation errors linked to the affected resource/line when available;
- revision history with summary, size, checksum, and restore action.

Saving a chapter, cover, metadata form, or spine order publishes one revision. The page refreshes its `baseRevision` after success.

## Android Integration

The Android catalog DTO includes `contentRevision`. When a downloaded book's remote revision changes, the app marks it as update available. Updating downloads the new EPUB transactionally, keeps the old local file until parsing succeeds, migrates the local locator using the fallback rules, then replaces the managed file.

Android does not edit EPUB content in this delivery. It only consumes revisions and migrates progress.

## Errors and Recovery

- Stale `baseRevision`: `409`, current revision returned.
- Invalid EPUB/XHTML/OPF or unsafe archive path: `400`, no publication.
- Unknown book/chapter/revision: `404`.
- Storage failure: `500`, current revision unchanged.
- Revision pruning failure: publication remains successful but cleanup is logged and retried.
- Restore failure: current revision unchanged.

Temporary directories are removed after success or failure. Startup/background cleanup removes abandoned workspaces and unreferenced generated objects older than a safety window.

## Testing and Acceptance

Unit and integration fixtures cover EPUB 2 and EPUB 3 books, nested chapter paths, existing and absent covers, XHTML with inline formatting, navigation documents, malformed input, unsafe ZIP paths, concurrent revision conflicts, revision pruning, and restore.

Acceptance requires:

1. replace a cover and verify preview plus downloaded EPUB;
2. change metadata and verify OPF plus server list values;
3. edit, add, reorder, and delete chapters while preserving a valid spine;
4. reject invalid XHTML without changing the current download;
5. restore a prior revision as a new revision;
6. update Android to the new revision and restore a locator from the old revision;
7. confirm original plus configured retained revisions and no abandoned temporary files.

## Non-goals

- Full WYSIWYG editing.
- Automatic repair of arbitrary broken EPUBs.
- Collaborative multi-user editing.
- Automatic rewriting of cross-chapter links after deletion.
- Android-side EPUB authoring.
