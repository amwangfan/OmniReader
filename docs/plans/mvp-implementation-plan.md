# OmniReader MVP Implementation Plan

Date: 2026-07-04

This plan implements the approved MVP design in small, testable slices. The first public repository should remain attributable to the repository owner's GitHub account; Codex usage is documented as workflow assistance, not authorship.

## Ground rules

- Build from `E:\Codex\Projects\OmniReader`.
- Keep the old `E:\Codex\Projects\boox-books-sync` repository intact.
- Prefer local builds and tests.
- Use the Aliyun host only for remote demo/deployment validation or when local verification is insufficient.
- Use simple SSH commands when Aliyun access is needed.
- Remote demo uses HTTP over Tailscale, not a public internet port.
- MVP web admin uses lightweight server-rendered pages.
- Server storage is local filesystem first, behind a storage interface.
- Android root and non-root behavior are identical in the first version.

## Phase 0 — Repository foundation

Deliverables:

- `.gitignore`
- license placeholder or selected license
- server and Android top-level directories
- developer documentation for local commands
- clear README statement about Codex-assisted workflow and non-authorship

Verification:

- `git status` is clean after committed changes.
- Git author remains the repository owner's GitHub identity.

## Phase 1 — Server foundation

Deliverables:

- Go module under `server/`
- `cmd/omnireader-server`
- config loading from env/file
- structured logging
- health endpoint
- SQLite connection and migrations
- local data directory initialization

Tests first:

- config default/override tests
- migration smoke test using a temporary SQLite database
- health endpoint test

Verification:

- `go test ./...`
- local server starts with temporary data directory
- `/healthz` returns OK

## Phase 2 — Authentication

Deliverables:

- admin user bootstrap
- password hashing
- login endpoint
- access token and refresh token issuing
- refresh and logout endpoints
- auth middleware
- minimal server-rendered login page

Tests first:

- password hash verify/fail cases
- login success/failure
- protected endpoint rejects anonymous requests
- refresh token rotation or revocation behavior

Verification:

- `go test ./...`
- browser login works locally
- anonymous book endpoint access is rejected

## Phase 3 — Books and storage

Deliverables:

- storage interface
- local filesystem storage adapter
- book database model
- EPUB upload endpoint/page
- list/search endpoint/page
- authenticated download endpoint
- explicit archive/delete behavior
- checksum and file size capture

Tests first:

- local storage save/read/delete tests
- upload rejects non-EPUB in MVP
- authenticated download succeeds
- anonymous download fails
- archive/delete does not silently delete unrelated files

Verification:

- `go test ./...`
- upload an EPUB through web admin
- list shows the uploaded book
- download works only while authenticated

## Phase 4 — Devices and reading progress

Deliverables:

- device registration/update endpoint
- progress read/write endpoint
- last-write-wins policy
- admin page showing devices and latest progress

Tests first:

- device upsert behavior
- progress update behavior
- progress requires authenticated client
- newer update replaces older visible state

Verification:

- `go test ./...`
- API can store progress for a fixture book/device
- web admin shows latest progress

## Phase 5 — Android foundation

Deliverables:

- Android Gradle project under `android/`
- Kotlin + Jetpack Compose app
- server URL configuration screen
- login screen
- token storage
- API client
- book list screen

Tests first where practical:

- API model serialization tests
- auth repository tests
- sync state reducer tests

Verification:

- Gradle build succeeds locally
- app can log in to the local server
- app can list server books

## Phase 6 — Android download, sync, and simple reader

Deliverables:

- local EPUB download into app storage
- startup sync
- manual sync
- periodic WorkManager sync
- simple EPUB reader screen
- chapter navigation
- local progress persistence
- progress upload

Tests first where practical:

- download state transitions
- local book repository behavior
- progress serialization
- sync conflict policy matches server MVP

Verification:

- app downloads an EPUB from local server
- app opens the downloaded EPUB
- progress survives app restart
- server receives progress update

## Phase 7 — Aliyun Tailscale HTTP demo

Deliverables:

- Linux server build
- minimal deployment directory under `/opt/omnireader`
- config file for demo
- systemd unit or simple controlled process
- documented removal command

Verification:

- service runs on Aliyun
- web admin is reachable over Tailscale HTTP
- upload/list/download/progress smoke test passes
- deletion command removes service files and data directory

## Phase 8 — Public GitHub repository

Deliverables:

- public GitHub repository named `OmniReader`
- pushed `main` branch
- README and design docs included
- no secrets, sample books, or private config committed

Verification:

- `git status` clean
- `git remote -v` points to the new repository
- GitHub page shows public repository
- repository contains no sensitive local paths beyond documentation context that is safe to publish

## Suggested first coding slice

Start with phases 0 and 1 only:

1. Add repository hygiene files.
2. Create the Go server module.
3. Add config, logging, health endpoint, SQLite migration skeleton, and tests.
4. Run `go test ./...`.
5. Commit before moving to auth.

