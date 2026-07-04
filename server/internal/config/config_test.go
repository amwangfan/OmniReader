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
	wantDB := filepath.Join(DefaultDataDir, "app.db")
	if cfg.DatabasePath != wantDB {
		t.Fatalf("database path = %q, want %q", cfg.DatabasePath, wantDB)
	}
}

func TestLoadAcceptsOverrides(t *testing.T) {
	values := map[string]string{
		"OMNIREADER_ADDR":          "0.0.0.0:9090",
		"OMNIREADER_DATA_DIR":      "/opt/omnireader/data",
		"OMNIREADER_DATABASE_PATH": "/opt/omnireader/data/custom.db",
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
	if cfg.DatabasePath != "/opt/omnireader/data/custom.db" {
		t.Fatalf("database path = %q", cfg.DatabasePath)
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
