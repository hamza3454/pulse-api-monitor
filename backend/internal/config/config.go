// Package config loads service configuration from environment variables
// (12-factor style). Every Pulse service reads its settings through here so
// defaults and validation live in exactly one place.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config holds the settings shared by the Pulse services. Fields are added as
// later phases need them (Kafka brokers, Redis address, ...).
type Config struct {
	HTTPAddr    string     // address the API listens on, e.g. ":8080"
	DatabaseURL string     // PostgreSQL connection string
	LogLevel    slog.Level // minimum level for structured logs
	CORSOrigin  string     // the one browser origin allowed to call the API

	// Workers is how many checks run at the same time inside this process.
	// 0 turns the built-in scheduler and workers off (the API still runs).
	Workers int
	// SchedulerTick is how often the scheduler looks for monitors that are due.
	SchedulerTick time.Duration
}

const (
	defaultHTTPAddr    = ":8080"
	defaultDatabaseURL = "postgres://pulse:pulse@localhost:5432/pulse?sslmode=disable"
	defaultLogLevel    = "info"
	defaultCORSOrigin  = "http://localhost:5173" // the Vite dev server

	defaultWorkers       = 10
	maxWorkers           = 1000
	defaultSchedulerTick = time.Second
	minSchedulerTick     = 100 * time.Millisecond
	maxSchedulerTick     = time.Minute
)

// Load reads configuration using getenv (pass os.Getenv in production, a fake
// in tests). Unset or empty variables fall back to development defaults.
func Load(getenv func(string) string) (Config, error) {
	level, err := parseLevel(valueOr(getenv("PULSE_LOG_LEVEL"), defaultLogLevel))
	if err != nil {
		return Config{}, err
	}

	workers, err := parseWorkers(getenv("PULSE_WORKERS"))
	if err != nil {
		return Config{}, err
	}
	tick, err := parseTick(getenv("PULSE_SCHEDULER_TICK"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:      valueOr(getenv("PULSE_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseURL:   valueOr(getenv("PULSE_DATABASE_URL"), defaultDatabaseURL),
		LogLevel:      level,
		CORSOrigin:    valueOr(getenv("PULSE_CORS_ORIGIN"), defaultCORSOrigin),
		Workers:       workers,
		SchedulerTick: tick,
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

func parseWorkers(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultWorkers, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 || n > maxWorkers {
		return 0, fmt.Errorf("invalid PULSE_WORKERS %q: want a whole number from 0 to %d", raw, maxWorkers)
	}
	return n, nil
}

func parseTick(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultSchedulerTick, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d < minSchedulerTick || d > maxSchedulerTick {
		return 0, fmt.Errorf("invalid PULSE_SCHEDULER_TICK %q: want a duration like 500ms or 1s, between %s and %s",
			raw, minSchedulerTick, maxSchedulerTick)
	}
	return d, nil
}
