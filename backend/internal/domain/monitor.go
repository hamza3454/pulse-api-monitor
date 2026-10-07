// Package domain holds Pulse's core types and business rules. It depends on
// nothing but the standard library, so every service can share it.
package domain

import (
	"errors"
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

// Validate reports every problem with the monitor's user-editable fields, or
// nil if it is valid. Problems are joined so the API can show them all at once.
func (m Monitor) Validate() error {
	var errs []error

	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, errors.New("name is required"))
	} else if len(m.Name) > MaxNameLength {
		errs = append(errs, fmt.Errorf("name must be at most %d characters", MaxNameLength))
	}

	if err := validateURL(m.URL); err != nil {
		errs = append(errs, err)
	}

	if m.Method != "GET" && m.Method != "HEAD" {
		errs = append(errs, errors.New("method must be GET or HEAD"))
	}

	if m.IntervalSeconds < MinIntervalSeconds || m.IntervalSeconds > MaxIntervalSeconds {
		errs = append(errs, fmt.Errorf("interval_seconds must be between %d and %d", MinIntervalSeconds, MaxIntervalSeconds))
	}

	if m.TimeoutSeconds < MinTimeoutSeconds || m.TimeoutSeconds > MaxTimeoutSeconds {
		errs = append(errs, fmt.Errorf("timeout_seconds must be between %d and %d", MinTimeoutSeconds, MaxTimeoutSeconds))
	} else if m.TimeoutSeconds > m.IntervalSeconds {
		errs = append(errs, errors.New("timeout_seconds must not exceed interval_seconds"))
	}

	if m.ExpectedStatus < 100 || m.ExpectedStatus > 599 {
		errs = append(errs, errors.New("expected_status must be between 100 and 599"))
	}

	return errors.Join(errs...)
}

func validateURL(raw string) error {
	if len(raw) > MaxURLLength {
		return fmt.Errorf("url must be at most %d characters", MaxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("url is not valid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("url must start with http:// or https://")
	}
	if u.Hostname() == "" {
		return errors.New("url must include a host")
	}
	return nil
}
