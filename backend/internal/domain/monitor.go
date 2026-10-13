// Package domain holds Pulse's core types and business rules. It depends on
// nothing but the standard library, so every service can share it.
package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Limits for monitor settings. The same bounds are enforced by CHECK
// constraints in migrations/000001_init.up.sql; keep the two in sync.
const (
	MinIntervalSeconds = 10
	MaxIntervalSeconds = 86400 // 24h
	MinTimeoutSeconds  = 1
	MaxTimeoutSeconds  = 60
	MaxNameLength      = 100
	MaxURLLength       = 2048
)

// Defaults applied by the API when a field is omitted on creation. They match
// the column defaults in the database.
const (
	DefaultMethod          = "GET"
	DefaultIntervalSeconds = 60
	DefaultTimeoutSeconds  = 10
	DefaultExpectedStatus  = 200
)

// Monitor is an API endpoint that Pulse checks on a schedule.
type Monitor struct {
	ID              string
	Name            string
	URL             string
	Method          string // GET or HEAD
	IntervalSeconds int
	TimeoutSeconds  int
	ExpectedStatus  int
	Enabled         bool
	NextCheckAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Validate checks the monitor's user-editable fields. It returns nil when the
// monitor is valid, otherwise a *ValidationError listing every problem.
func (m Monitor) Validate() error {
	var problems []string

	if strings.TrimSpace(m.Name) == "" {
		problems = append(problems, "name is required")
	} else if len(m.Name) > MaxNameLength {
		problems = append(problems, fmt.Sprintf("name must be at most %d characters", MaxNameLength))
	}

	if p := validateURL(m.URL); p != "" {
		problems = append(problems, p)
	}

	if m.Method != "GET" && m.Method != "HEAD" {
		problems = append(problems, "method must be GET or HEAD")
	}

	if m.IntervalSeconds < MinIntervalSeconds || m.IntervalSeconds > MaxIntervalSeconds {
		problems = append(problems, fmt.Sprintf("interval_seconds must be between %d and %d", MinIntervalSeconds, MaxIntervalSeconds))
	}

	if m.TimeoutSeconds < MinTimeoutSeconds || m.TimeoutSeconds > MaxTimeoutSeconds {
		problems = append(problems, fmt.Sprintf("timeout_seconds must be between %d and %d", MinTimeoutSeconds, MaxTimeoutSeconds))
	} else if m.TimeoutSeconds > m.IntervalSeconds {
		problems = append(problems, "timeout_seconds must not exceed interval_seconds")
	}

	if m.ExpectedStatus < 100 || m.ExpectedStatus > 599 {
		problems = append(problems, "expected_status must be between 100 and 599")
	}

	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

// validateURL returns a description of what is wrong with raw, or "" if it is
// an acceptable monitor URL.
func validateURL(raw string) string {
	if len(raw) > MaxURLLength {
		return fmt.Sprintf("url must be at most %d characters", MaxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "url is not valid"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "url must start with http:// or https://"
	}
	if u.Hostname() == "" {
		return "url must include a host"
	}
	return ""
}
