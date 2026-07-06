package epubedit

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
)

func TestUpdateMetadataPreservesUnknownMetadata(t *testing.T) {
	workspace := openTestEPUB(t)
	defer workspace.Close()
	err := workspace.UpdateMetadata(Metadata{Title: "Changed", Author: "New Author", Language: "zh-CN", Publisher: "Press", Description: "Summary", Identifier: "id-2"})
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zipBody(t, rebuilt, "OPS/content.opf")), "custom:test") {
		t.Fatal("unknown metadata was discarded")
	}
	reopened, err := Open(rebuilt, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.Content().Metadata; got.Title != "Changed" || got.Author != "New Author" || got.Publisher != "Press" || got.Identifier != "id-2" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestChapterSourceUpdateAddDeleteAndReorder(t *testing.T) {
	workspace := openTestEPUB(t)
	defer workspace.Close()
	source, err := workspace.ChapterSource("chapter-one")
	if err != nil || !strings.Contains(source, "Hello") {
		t.Fatalf("ChapterSource = %q, %v", source, err)
	}
	updated := `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Renamed</title></head><body><p>Edited <em>inline</em></p></body></html>`
	if err := workspace.UpdateChapter("chapter-one", updated, "Renamed"); err != nil {
		t.Fatal(err)
	}
	added, err := workspace.AddChapter("Added", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Added</title></head><body><p>New</p></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.ReorderSpine([]string{added.ID, "chapter-two", "chapter-one"}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.DeleteChapter("chapter-two"); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(rebuilt, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	chapters := reopened.Content().Chapters
	if len(chapters) != 2 || chapters[0].ID != added.ID || chapters[1].ID != "chapter-one" || chapters[1].Title != "Renamed" {
		t.Fatalf("chapters = %#v", chapters)
	}
	source, err = reopened.ChapterSource("chapter-one")
	if err != nil || !strings.Contains(source, "Edited <em>inline</em>") {
		t.Fatalf("updated source = %q, %v", source, err)
	}
}

func TestChapterMutationsRejectUnsafeInvalidOrStaleInput(t *testing.T) {
	workspace := openTestEPUB(t)
	defer workspace.Close()
	bad := []string{
		`<html><body>`,
		`<html xmlns="http://www.w3.org/1999/xhtml"><body><script>x</script></body></html>`,
		`<html xmlns="http://www.w3.org/1999/xhtml"><body><img src="../../../escape.png"/></body></html>`,
	}
	for _, source := range bad {
		if err := workspace.UpdateChapter("chapter-one", source, ""); err == nil {
			t.Fatalf("accepted unsafe source %q", source)
		}
	}
	if err := workspace.UpdateChapter("chapter-one", `<html xmlns="http://www.w3.org/1999/xhtml"><body><img src="../images/inside.png"/></body></html>`, ""); err != nil {
		t.Fatalf("rejected a parent reference that remains inside the EPUB: %v", err)
	}
	if err := workspace.UpdateChapter("stale", `<html/>`, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale chapter error = %v", err)
	}
	if err := workspace.ReorderSpine([]string{"chapter-one", "chapter-one"}); err == nil {
		t.Fatal("accepted duplicate/incomplete spine")
	}
	if err := workspace.DeleteChapter("chapter-one"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.DeleteChapter("chapter-two"); err == nil {
		t.Fatal("deleted final spine item")
	}
}

func TestReplaceCoverDecodesContentAndAddsManifestItem(t *testing.T) {
	workspace := openTestEPUB(t)
	defer workspace.Close()
	var imageBody bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageBody, img); err != nil {
		t.Fatal(err)
	}
	cover, err := workspace.ReplaceCover(imageBody.Bytes(), CoverLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if cover.MediaType != "image/png" || cover.Width != 3 || cover.Height != 2 || len(cover.Data) == 0 {
		t.Fatalf("cover = %#v", cover)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	opf := string(zipBody(t, rebuilt, "OPS/content.opf"))
	if !strings.Contains(opf, `properties="cover-image"`) || !strings.Contains(opf, `media-type="image/png"`) {
		t.Fatalf("cover manifest not updated: %s", opf)
	}
}

func TestUpdateChapterTitleUpdatesEPUB3Navigation(t *testing.T) {
	workspace, err := Open(testEPUB(t, testEPUBOptions{withNav: true}), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	source, err := workspace.ChapterSource("chapter-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.UpdateChapter("chapter-one", source, "Renamed"); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	nav := string(zipBody(t, rebuilt, "OPS/nav.xhtml"))
	if !strings.Contains(nav, ">Renamed</a>") {
		t.Fatalf("navigation title not updated: %s", nav)
	}
}

func TestReplaceCoverUsesEPUB2CoverMetadata(t *testing.T) {
	var imageBody bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&imageBody, img); err != nil {
		t.Fatal(err)
	}
	data := rawZIP(t, map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/book.opf"/></rootfiles></container>`,
		"OEBPS/book.opf":         `<package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Old</dc:title><dc:creator>A</dc:creator><meta name="cover" content="legacy-cover"/></metadata><manifest><item id="legacy-cover" href="images/old.jpg" media-type="image/jpeg"/><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/></spine></package>`,
		"OEBPS/one.xhtml":        `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><p>x</p></body></html>`,
		"OEBPS/images/old.jpg":   "old",
	})
	workspace, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	cover, err := workspace.ReplaceCover(imageBody.Bytes(), CoverLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if cover.Href != "images/old.jpg" {
		t.Fatalf("cover href=%q", cover.Href)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	opf := string(zipBody(t, rebuilt, "OEBPS/book.opf"))
	if !strings.Contains(opf, `id="legacy-cover"`) || !strings.Contains(opf, `media-type="image/png"`) {
		t.Fatalf("EPUB2 cover metadata lost: %s", opf)
	}
}

func TestReplaceCoverCreatesEPUB2CoverMetadataWhenAbsent(t *testing.T) {
	var imageBody bytes.Buffer
	if err := png.Encode(&imageBody, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := rawZIP(t, map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Old</dc:title></metadata><manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/></spine></package>`,
		"one.xhtml":              `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><p>x</p></body></html>`,
	})
	workspace, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	if _, err := workspace.ReplaceCover(imageBody.Bytes(), CoverLimits{}); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := workspace.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	opf := string(zipBody(t, rebuilt, "book.opf"))
	if !strings.Contains(opf, `name="cover"`) || !strings.Contains(opf, `content="cover-image"`) {
		t.Fatalf("EPUB2 cover meta not created: %s", opf)
	}
}

func openTestEPUB(t *testing.T) *Workspace {
	t.Helper()
	workspace, err := Open(testEPUB(t, testEPUBOptions{}), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func zipBody(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		body, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer body.Close()
		result, err := io.ReadAll(body)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	t.Fatalf("zip entry %q not found", name)
	return nil
}
