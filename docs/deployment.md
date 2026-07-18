# Temporary Linux deployment

OmniReader stores application state in one SQLite database and one books
directory. A temporary deployment should run behind HTTPS or be reachable only
through a private network such as Tailscale.

## Runtime requirements

- 64-bit Linux capable of running the Go server binary.
- Calibre `ebook-convert` for MOBI, AZW, AZW3, TXT, PDF, HTML, and HTM input.
- A dedicated unprivileged service account.
- A persistent data directory with enough temporary space for conversion.

Install Calibre using its official Linux instructions, then verify:

```bash
ebook-convert --version
```

PDF conversion uses the document's available text and layout. Scanned PDFs need
OCR before upload, and complex multi-column PDFs can require manual cleanup.

## Required environment

```text
OMNIREADER_ADDR=127.0.0.1:18080
OMNIREADER_DATA_DIR=/opt/omnireader/data
OMNIREADER_ADMIN_USERNAME=admin
OMNIREADER_ADMIN_PASSWORD=<long-random-password>
OMNIREADER_TOKEN_SECRET=<at-least-32-random-bytes>
OMNIREADER_EBOOK_CONVERT=/usr/bin/ebook-convert
```

Keep the password and token secret outside the repository. Bind to loopback
when a reverse proxy or Tailscale proxy exposes the service.

## Verification

1. Confirm `GET /healthz` returns `status: ok`.
2. Log in and confirm `GET /api/v1/conversion` reports `available: true`.
3. Upload one TXT and one MOBI or PDF fixture.
4. Download each result and validate that it is an EPUB ZIP containing
   `META-INF/container.xml` and a package document.
5. Confirm title/author search and `GET /api/v1/books/{bookId}`.
6. Remove the temporary account, process, data directory, and proxy route after
   validation if the deployment is no longer needed.

Run Calibre and OmniReader under resource limits for an internet-reachable test
server. File conversion processes untrusted documents and should not run as
root.
