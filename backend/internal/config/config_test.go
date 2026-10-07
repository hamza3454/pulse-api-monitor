package config

import (
	"log/slog"
	"testing"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envFrom(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL != defaultDatabaseURL {
		t.Errorf("DatabaseURL = %q, want default", cfg.DatabaseURL)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"PULSE_HTTP_ADDR":    ":9090",
		"PULSE_DATABASE_URL": "postgres://u:p@db:5432/x",
		"PULSE_LOG_LEVEL":    "debug",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":9090" || cfg.DatabaseURL != "postgres://u:p@db:5432/x" || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadBlankFallsBack(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"PULSE_HTTP_ADDR": "   "}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("blank value should fall back, got %q", cfg.HTTPAddr)
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	if _, err := Load(envFrom(map[string]string{"PULSE_LOG_LEVEL": "loud"})); err == nil {
		t.Fatal("expected error for invalid log level")
	}
}
