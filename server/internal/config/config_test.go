package config

import (
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	cfg := Load(func(string) (string, bool) {
		return "", false
	})

	if cfg.Addr != DefaultAddr {
		t.Fatalf("addr = %q, want %q", cfg.Addr, DefaultAddr)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Fatalf("data dir = %q, want %q", cfg.DataDir, DefaultDataDir)
	}
	if cfg.BooksDir != filepath.Join(DefaultDataDir, "books") {
		t.Fatalf("books dir = %q", cfg.BooksDir)
	}
	wantDB := filepath.Join(DefaultDataDir, "app.db")
	if cfg.DatabasePath != wantDB {
		t.Fatalf("database path = %q, want %q", cfg.DatabasePath, wantDB)
	}
	if cfg.AdminUsername != DefaultAdmin {
		t.Fatalf("admin username = %q, want %q", cfg.AdminUsername, DefaultAdmin)
	}
	if cfg.EbookConvertPath != DefaultEbookConvert {
		t.Fatalf("ebook convert path = %q", cfg.EbookConvertPath)
	}
}

func TestLoadAcceptsOverrides(t *testing.T) {
	values := map[string]string{
		"OMNIREADER_ADDR":           "0.0.0.0:9090",
		"OMNIREADER_DATA_DIR":       "/opt/omnireader/data",
		"OMNIREADER_BOOKS_DIR":      "/opt/omnireader/data/library",
		"OMNIREADER_DATABASE_PATH":  "/opt/omnireader/data/custom.db",
		"OMNIREADER_ADMIN_USERNAME": "owner",
		"OMNIREADER_ADMIN_PASSWORD": "secret-password",
		"OMNIREADER_TOKEN_SECRET":   "token-secret",
		"OMNIREADER_EBOOK_CONVERT":  "/opt/calibre/ebook-convert",
	}

	cfg := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})

	if cfg.Addr != "0.0.0.0:9090" {
		t.Fatalf("addr = %q", cfg.Addr)
	}
	if cfg.DataDir != "/opt/omnireader/data" {
		t.Fatalf("data dir = %q", cfg.DataDir)
	}
	if cfg.BooksDir != "/opt/omnireader/data/library" {
		t.Fatalf("books dir = %q", cfg.BooksDir)
	}
	if cfg.DatabasePath != "/opt/omnireader/data/custom.db" {
		t.Fatalf("database path = %q", cfg.DatabasePath)
	}
	if cfg.AdminUsername != "owner" {
		t.Fatalf("admin username = %q", cfg.AdminUsername)
	}
	if cfg.AdminPassword != "secret-password" {
		t.Fatalf("admin password = %q", cfg.AdminPassword)
	}
	if cfg.TokenSecret != "token-secret" {
		t.Fatalf("token secret = %q", cfg.TokenSecret)
	}
	if cfg.EbookConvertPath != "/opt/calibre/ebook-convert" {
		t.Fatalf("ebook convert path = %q", cfg.EbookConvertPath)
	}
}

func TestLoadIgnoresBlankOverrides(t *testing.T) {
	cfg := Load(func(key string) (string, bool) {
		if key == "OMNIREADER_ADDR" {
			return "   ", true
		}
		return "", false
	})

	if cfg.Addr != DefaultAddr {
		t.Fatalf("addr = %q, want default %q", cfg.Addr, DefaultAddr)
	}
}
