package books

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseEPUBMetadata(t *testing.T) {
	epub := fixtureEPUB(t, "The Parsed Book", "The Parsed Author")

	metadata, err := ParseEPUBMetadata(epub)
	if err != nil {
		t.Fatalf("ParseEPUBMetadata returned error: %v", err)
	}
	if metadata.Title != "The Parsed Book" {
		t.Fatalf("title = %q", metadata.Title)
	}
	if metadata.Author != "The Parsed Author" {
		t.Fatalf("author = %q", metadata.Author)
	}
}

func fixtureEPUB(t *testing.T, title string, author string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	addZipFile(t, writer, "META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`)
	addZipFile(t, writer, "OPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="bookid" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>`+title+`</dc:title>
    <dc:creator>`+author+`</dc:creator>
  </metadata>
</package>`)
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buffer.Bytes()
}

func addZipFile(t *testing.T, writer *zip.Writer, name string, body string) {
	t.Helper()
	file, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip file %s: %v", name, err)
	}
	if _, err := file.Write([]byte(body)); err != nil {
		t.Fatalf("write zip file %s: %v", name, err)
	}
}
