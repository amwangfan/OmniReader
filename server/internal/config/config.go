package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAddr    = "127.0.0.1:8080"
	DefaultDataDir = "data"
	DefaultAdmin   = "admin"
	DefaultEbookConvert = "ebook-convert"
)

type Config struct {
	Addr          string
	DataDir       string
	BooksDir      string
	DatabasePath  string
	AdminUsername string
	AdminPassword string
	TokenSecret   string
	EbookConvertPath string
}

func LoadFromEnv() Config {
	return Load(os.LookupEnv)
}

func Load(lookup func(string) (string, bool)) Config {
	addr := valueOrDefault(lookup, "OMNIREADER_ADDR", DefaultAddr)
	dataDir := valueOrDefault(lookup, "OMNIREADER_DATA_DIR", DefaultDataDir)
	booksDir := valueOrDefault(lookup, "OMNIREADER_BOOKS_DIR", filepath.Join(dataDir, "books"))
	databasePath := valueOrDefault(lookup, "OMNIREADER_DATABASE_PATH", filepath.Join(dataDir, "app.db"))
	adminUsername := valueOrDefault(lookup, "OMNIREADER_ADMIN_USERNAME", DefaultAdmin)
	adminPassword := valueOrDefault(lookup, "OMNIREADER_ADMIN_PASSWORD", "")
	tokenSecret := valueOrDefault(lookup, "OMNIREADER_TOKEN_SECRET", "")
	ebookConvertPath := valueOrDefault(lookup, "OMNIREADER_EBOOK_CONVERT", DefaultEbookConvert)

	return Config{
		Addr:          addr,
		DataDir:       dataDir,
		BooksDir:      booksDir,
		DatabasePath:  databasePath,
		AdminUsername: adminUsername,
		AdminPassword: adminPassword,
		TokenSecret:   tokenSecret,
		EbookConvertPath: ebookConvertPath,
	}
}

func valueOrDefault(lookup func(string) (string, bool), key string, fallback string) string {
	value, ok := lookup(key)
	if !ok {
		return fallback
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
