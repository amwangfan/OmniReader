package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalSaveOpenDelete(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal returned error: %v", err)
	}

	if err := store.Save(ctx, "books/book-1/original.epub", strings.NewReader("epub")); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	reader, err := store.Open(ctx, "books/book-1/original.epub")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if string(body) != "epub" {
		t.Fatalf("body = %q", body)
	}

	if err := store.Delete(ctx, "books/book-1/original.epub"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := store.Open(ctx, "books/book-1/original.epub"); err == nil {
		t.Fatal("expected deleted object to be unavailable")
	}
}

func TestLocalRejectsPathTraversal(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal returned error: %v", err)
	}
	if err := store.Save(context.Background(), "../secret", strings.NewReader("nope")); err == nil {
		t.Fatal("expected path traversal key to fail")
	}
}
