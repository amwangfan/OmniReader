package epubedit

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestOpenInspectsEPUB3AndCleansWorkspace(t *testing.T) {
	data := testEPUB(t, testEPUBOptions{})
	workspace, err := Open(data, Limits{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	root := workspace.Root()
	content := workspace.Content()
	if content.Metadata.Title != "Example" || content.Metadata.Author != "Writer" {
		t.Fatalf("metadata = %#v", content.Metadata)
	}
	if len(content.Chapters) != 2 || content.Chapters[0].ID != "chapter-one" || content.Chapters[0].Href != "text/one.xhtml" {
		t.Fatalf("chapters = %#v", content.Chapters)
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists after Close: %v", err)
	}
}

func TestOpenRejectsUnsafeOrOversizedArchives(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		limits Limits
	}{
		{"zip slip", rawZIP(t, map[string]string{"../escape": "bad"}), Limits{}},
		{"backslash escape", rawZIP(t, map[string]string{"..\\escape": "bad"}), Limits{}},
		{"too many entries", rawZIP(t, map[string]string{"a": "a", "b": "b"}), Limits{MaxEntries: 1}},
		{"expanded size", rawZIP(t, map[string]string{"a": strings.Repeat("x", 20)}), Limits{MaxExpandedBytes: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if workspace, err := Open(tt.data, tt.limits); err == nil {
				_ = workspace.Close()
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestOpenRejectsMalformedOrExecutableEPUB(t *testing.T) {
	tests := []struct {
		name string
		opts testEPUBOptions
	}{
		{"malformed container", testEPUBOptions{container: `<container>`}},
		{"missing manifest item", testEPUBOptions{spine: `<itemref idref="missing"/>`}},
		{"malformed xhtml", testEPUBOptions{chapterOne: `<html><body>`}},
		{"script", testEPUBOptions{chapterOne: `<html xmlns="http://www.w3.org/1999/xhtml"><body><script>alert(1)</script></body></html>`}},
		{"event handler", testEPUBOptions{chapterOne: `<html xmlns="http://www.w3.org/1999/xhtml"><body onload="alert(1)">x</body></html>`}},
		{"escaping reference", testEPUBOptions{chapterOne: `<html xmlns="http://www.w3.org/1999/xhtml"><body><img src="../../../escape.png"/></body></html>`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if workspace, err := Open(testEPUB(t, tt.opts), Limits{}); err == nil {
				_ = workspace.Close()
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestRebuildWritesStoredMimetypeFirstAndRevalidates(t *testing.T) {
	workspace, err := Open(testEPUB(t, testEPUBOptions{}), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	data, err := workspace.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) == 0 || reader.File[0].Name != "mimetype" || reader.File[0].Method != zip.Store {
		t.Fatalf("first entry = %#v", reader.File[0])
	}
	reopened, err := Open(data, Limits{})
	if err != nil {
		t.Fatalf("rebuilt EPUB did not revalidate: %v", err)
	}
	_ = reopened.Close()
}

type testEPUBOptions struct {
	container  string
	spine      string
	chapterOne string
}

func testEPUB(t *testing.T, opts testEPUBOptions) []byte {
	t.Helper()
	container := opts.container
	if container == "" {
		container = `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`
	}
	spine := opts.spine
	if spine == "" {
		spine = `<itemref idref="chapter-one"/><itemref idref="chapter-two"/>`
	}
	one := opts.chapterOne
	if one == "" {
		one = `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><h1>One</h1><p>Hello <em>world</em>.</p></body></html>`
	}
	return rawZIP(t, map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": container,
		"OPS/content.opf":        `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Example</dc:title><dc:creator>Writer</dc:creator><dc:language>en</dc:language><meta property="custom:test">keep</meta></metadata><manifest><item id="chapter-one" href="text/one.xhtml" media-type="application/xhtml+xml"/><item id="chapter-two" href="text/two.xhtml" media-type="application/xhtml+xml"/></manifest><spine>` + spine + `</spine></package>`,
		"OPS/text/one.xhtml":     one,
		"OPS/text/two.xhtml":     `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Two</title></head><body><p>Second</p></body></html>`,
	})
}

func rawZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
