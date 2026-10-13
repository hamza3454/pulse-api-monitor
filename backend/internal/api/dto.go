package api

import (
	"time"

	"pulse/internal/domain"
)

// Request and response shapes are separate from domain.Monitor so the JSON
// contract (snake_case, optional fields) can evolve without touching the
// core types.

// createMonitorRequest is the body of POST /api/v1/monitors. Pointer fields
// are optional: nil means "use the default".
type createMonitorRequest struct {
	Name            string  `json:"name"`
	URL             string  `json:"url"`
	Method          *string `json:"method"`
	IntervalSeconds *int    `json:"interval_seconds"`
	TimeoutSeconds  *int    `json:"timeout_seconds"`
	ExpectedStatus  *int    `json:"expected_status"`
	Enabled         *bool   `json:"enabled"`
}

// updateMonitorRequest is the body of PATCH /api/v1/monitors/{id}. Only the
// fields present in the JSON are changed.
type updateMonitorRequest struct {
	Name            *string `json:"name"`
	URL             *string `json:"url"`
	Method          *string `json:"method"`
	IntervalSeconds *int    `json:"interval_seconds"`
	TimeoutSeconds  *int    `json:"timeout_seconds"`
	ExpectedStatus  *int    `json:"expected_status"`
	Enabled         *bool   `json:"enabled"`
}

type monitorResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	Method          string    `json:"method"`
	IntervalSeconds int       `json:"interval_seconds"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	ExpectedStatus  int       `json:"expected_status"`
	Enabled         bool      `json:"enabled"`
	NextCheckAt     time.Time `json:"next_check_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// listMonitorsResponse wraps the array in an object so fields such as paging
// can be added later without breaking clients.
type listMonitorsResponse struct {
	Monitors []monitorResponse `json:"monitors"`
}

func toMonitorResponse(m domain.Monitor) monitorResponse {
	return monitorResponse{
		ID:              m.ID,
		Name:            m.Name,
		URL:             m.URL,
		Method:          m.Method,
		IntervalSeconds: m.IntervalSeconds,
		TimeoutSeconds:  m.TimeoutSeconds,
		ExpectedStatus:  m.ExpectedStatus,
		Enabled:         m.Enabled,
		NextCheckAt:     m.NextCheckAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

// valueOr returns *p, or fallback when p is nil.
func valueOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}
