package books

import (
	"testing"
	"time"
)

func TestRenderFilenameTemplateDefault(t *testing.T) {
	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	got := RenderFilenameTemplate("", "Book: Name", "A/Writer", now)
	if got != "Book_ Name-A_Writer.epub" {
		t.Fatalf("filename = %q", got)
	}
}

func TestRenderFilenameTemplateDefaultWithoutAuthor(t *testing.T) {
	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	got := RenderFilenameTemplate("", "Book", "", now)
	if got != "Book.epub" {
		t.Fatalf("filename = %q", got)
	}
}

func TestRenderFilenameTemplateCustom(t *testing.T) {
	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	got := RenderFilenameTemplate("{{YYMMDD}}-{{Book}}-{{Author}}-123", "Book", "Author", now)
	if got != "260704-Book-Author-123.epub" {
		t.Fatalf("filename = %q", got)
	}
}
