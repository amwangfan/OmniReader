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

## Local development

Server commands, once Go is installed locally:

```powershell
cd E:\Codex\Projects\OmniReader\server
go test ./...
go run ./cmd/omnireader-server
```

The default server data directory is `server\data`, and runtime data is intentionally ignored by Git.
