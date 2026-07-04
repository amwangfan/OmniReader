# OmniReader

Experimental rewrite of `amwangfan/boox-books-sync` from a Magisk/rclone based BOOX sync module into a single-user server + Android reading app.

The first version targets:

- a lightweight self-hosted server for book storage, progress sync, web administration, and future downloader/plugin integration;
- a native Android app for BOOX and other Android devices, using Kotlin and Jetpack Compose;
- EPUB-first reading and synchronization;
- local filesystem storage first, with an interface reserved for OSS/S3-compatible backends.

The design starts from the existing local repository at:

`E:\Codex\Projects\boox-books-sync`

## Authorship and AI-assisted development

This project is authored and owned by the repository owner's GitHub account. Codex is used as an end-to-end engineering assistant for design, planning, implementation, testing, and documentation, but Codex is not the code author.

See [MVP design](docs/design/mvp-design.md).

Android client source lives in [`android/`](android/). It is a native Kotlin + Jetpack Compose APK targeting the first usable OmniReader app flow.

## Local development

Server commands, once Go 1.25 or newer is installed locally:

```powershell
cd E:\Codex\Projects\OmniReader\server
go test ./...
$env:OMNIREADER_ADMIN_PASSWORD='change-me'
$env:OMNIREADER_TOKEN_SECRET='local-dev-token-secret'
go run ./cmd/omnireader-server
```

The default server data directory is `server\data`, and runtime data is intentionally ignored by Git.

Current server MVP features:

- login-protected web admin;
- EPUB upload with automatic title/author extraction from EPUB metadata;
- configurable saved filename pattern such as `{{YYMMDD}}-{{Book}}-{{Author}}-123`;
- authenticated book list and download APIs;
- server-side book deletion;
- admin password change from the settings page.
