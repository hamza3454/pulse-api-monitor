package config

import (
	"log/slog"
	"testing"
	"time"
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
	if cfg.CORSOrigin != "http://localhost:5173" {
		t.Errorf("CORSOrigin = %q, want the Vite dev origin", cfg.CORSOrigin)
	}
	if cfg.Workers != 10 || cfg.SchedulerTick != time.Second {
		t.Errorf("Workers = %d, SchedulerTick = %v; want 10 and 1s", cfg.Workers, cfg.SchedulerTick)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"PULSE_HTTP_ADDR":      ":9090",
		"PULSE_DATABASE_URL":   "postgres://u:p@db:5432/x",
		"PULSE_LOG_LEVEL":      "debug",
		"PULSE_CORS_ORIGIN":    "https://pulse.example.com",
		"PULSE_WORKERS":        "25",
		"PULSE_SCHEDULER_TICK": "250ms",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":9090" || cfg.DatabaseURL != "postgres://u:p@db:5432/x" || cfg.LogLevel != slog.LevelDebug ||
		cfg.CORSOrigin != "https://pulse.example.com" || cfg.Workers != 25 || cfg.SchedulerTick != 250*time.Millisecond {
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

func TestLoadWorkersZeroDisablesMonitoring(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"PULSE_WORKERS": "0"}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Workers != 0 {
		t.Errorf("Workers = %d, want 0 (explicit zero must not fall back to the default)", cfg.Workers)
	}
}

func TestLoadRejectsBadWorkerAndTickValues(t *testing.T) {
	bad := []map[string]string{
		{"PULSE_WORKERS": "many"},
		{"PULSE_WORKERS": "-1"},
		{"PULSE_WORKERS": "1001"},
		{"PULSE_WORKERS": "2.5"},
		{"PULSE_SCHEDULER_TICK": "fast"},
		{"PULSE_SCHEDULER_TICK": "10ms"},
		{"PULSE_SCHEDULER_TICK": "2h"},
		{"PULSE_SCHEDULER_TICK": "-1s"},
		{"PULSE_SCHEDULER_TICK": "5"}, // a bare number has no unit
	}
	for _, env := range bad {
		if _, err := Load(envFrom(env)); err == nil {
			t.Errorf("Load(%v) succeeded, want an error", env)
		}
	}
}
