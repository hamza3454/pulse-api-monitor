// Package config loads service configuration from environment variables
// (12-factor style). Every Pulse service reads its settings through here so
// defaults and validation live in exactly one place.
package config

import (
	"fmt"
	"log/slog"
	"strings"
)

// Config holds the settings shared by the Pulse services. Fields are added as
// later phases need them (Kafka brokers, Redis address, ...).
type Config struct {
	HTTPAddr    string     // address the API listens on, e.g. ":8080"
	DatabaseURL string     // PostgreSQL connection string
	LogLevel    slog.Level // minimum level for structured logs
}

const (
	defaultHTTPAddr    = ":8080"
	defaultDatabaseURL = "postgres://pulse:pulse@localhost:5432/pulse?sslmode=disable"
	defaultLogLevel    = "info"
)

// Load reads configuration using getenv (pass os.Getenv in production, a fake
// in tests). Unset or empty variables fall back to development defaults.
func Load(getenv func(string) string) (Config, error) {
	level, err := parseLevel(valueOr(getenv("PULSE_LOG_LEVEL"), defaultLogLevel))
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:    valueOr(getenv("PULSE_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseURL: valueOr(getenv("PULSE_DATABASE_URL"), defaultDatabaseURL),
		LogLevel:    level,
	}, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func parseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("invalid PULSE_LOG_LEVEL %q: want debug, info, warn or error", s)
	}
	return l, nil
}
