package books

import (
	"context"
	"testing"
)

func TestSourceFormatAndSupportedFormats(t *testing.T) {
	if got := sourceFormat("My.Book.AZW3"); got != "azw3" {
		t.Fatalf("sourceFormat = %q", got)
	}
	for _, format := range []string{"epub", "mobi", "azw", "azw3", "txt", "pdf", "html", "htm"} {
		if !isSupportedSourceFormat(format) {
			t.Fatalf("expected %q to be supported", format)
		}
	}
	if isSupportedSourceFormat("docx") {
		t.Fatal("docx should not be supported")
	}
}

func TestMissingCalibreReportsUnavailable(t *testing.T) {
	converter := NewCalibreConverter("definitely-not-an-installed-ebook-convert")
	if converter.Status().Available {
		t.Fatal("missing converter should report unavailable")
	}
	if _, err := converter.Convert(t.Context(), "book.pdf", []byte("pdf")); err == nil {
		t.Fatal("missing converter should reject conversion")
	}
}

func TestInstalledCalibreConvertsTXTToEPUB(t *testing.T) {
	converter := NewCalibreConverter("ebook-convert")
	if !converter.Status().Available {
		t.Skip("ebook-convert is not installed")
	}
	converted, err := converter.Convert(
		context.Background(),
		"Smoke Test.txt",
		[]byte("Smoke Test\n\nThis plain text chapter must become a readable EPUB."),
	)
	if err != nil {
		t.Fatalf("Convert returned error: %v", err)
	}
	metadata, err := ParseEPUBMetadata(converted)
	if err != nil {
		t.Fatalf("converted output is not a valid EPUB: %v", err)
	}
	if metadata.Title == "" {
		t.Fatal("converted EPUB should contain a title")
	}
}
