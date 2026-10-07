package domain

import (
	"strings"
	"testing"
)

func validMonitor() Monitor {
	return Monitor{
		Name:            "Example API",
		URL:             "https://api.example.com/health",
		Method:          "GET",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		ExpectedStatus:  200,
		Enabled:         true,
	}
}

func TestMonitorValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Monitor)
		wantErr string // substring; empty means valid
	}{
		{"valid", func(m *Monitor) {}, ""},
		{"valid HEAD", func(m *Monitor) { m.Method = "HEAD" }, ""},
		{"valid with port and query", func(m *Monitor) { m.URL = "http://localhost:8080/ok?x=1" }, ""},
		{"blank name", func(m *Monitor) { m.Name = "  " }, "name is required"},
		{"name too long", func(m *Monitor) { m.Name = strings.Repeat("a", MaxNameLength+1) }, "name must be at most"},
		{"ftp scheme", func(m *Monitor) { m.URL = "ftp://example.com" }, "http:// or https://"},
		{"no scheme", func(m *Monitor) { m.URL = "example.com/health" }, "http:// or https://"},
		{"no host", func(m *Monitor) { m.URL = "https://" }, "include a host"},
		{"garbage url", func(m *Monitor) { m.URL = "http://[::1" }, "not valid"},
		{"url too long", func(m *Monitor) { m.URL = "https://example.com/" + strings.Repeat("a", MaxURLLength) }, "url must be at most"},
		{"bad method", func(m *Monitor) { m.Method = "POST" }, "method must be"},
		{"interval too small", func(m *Monitor) { m.IntervalSeconds = 5; m.TimeoutSeconds = 5 }, "interval_seconds"},
		{"interval too large", func(m *Monitor) { m.IntervalSeconds = MaxIntervalSeconds + 1 }, "interval_seconds"},
		{"timeout zero", func(m *Monitor) { m.TimeoutSeconds = 0 }, "timeout_seconds must be between"},
		{"timeout too large", func(m *Monitor) { m.TimeoutSeconds = 61; m.IntervalSeconds = 120 }, "timeout_seconds must be between"},
		{"timeout exceeds interval", func(m *Monitor) { m.IntervalSeconds = 10; m.TimeoutSeconds = 15 }, "must not exceed interval_seconds"},
		{"status too low", func(m *Monitor) { m.ExpectedStatus = 99 }, "expected_status"},
		{"status too high", func(m *Monitor) { m.ExpectedStatus = 600 }, "expected_status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMonitor()
			tt.mutate(&m)
			err := m.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestMonitorValidateReportsAllProblems(t *testing.T) {
	m := validMonitor()
	m.Name = ""
	m.Method = "DELETE"
	m.ExpectedStatus = 0

	err := m.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"name is required", "method must be", "expected_status"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}
