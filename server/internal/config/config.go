package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAddr    = "127.0.0.1:8080"
	DefaultDataDir = "data"
)

type Config struct {
	Addr         string
	DataDir      string
	DatabasePath string
}

func LoadFromEnv() Config {
	return Load(os.LookupEnv)
}

func Load(lookup func(string) (string, bool)) Config {
	addr := valueOrDefault(lookup, "OMNIREADER_ADDR", DefaultAddr)
	dataDir := valueOrDefault(lookup, "OMNIREADER_DATA_DIR", DefaultDataDir)
	databasePath := valueOrDefault(lookup, "OMNIREADER_DATABASE_PATH", filepath.Join(dataDir, "app.db"))

	return Config{
		Addr:         addr,
		DataDir:      dataDir,
		DatabasePath: databasePath,
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

