package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"pulse/internal/domain"
)

// MonitorStore is what the handlers need from persistence. It is defined here,
// where it is used, so tests can supply an in-memory fake and the Postgres
// store satisfies it without knowing about this package.
type MonitorStore interface {
	Create(ctx context.Context, m domain.Monitor) (domain.Monitor, error)
	Get(ctx context.Context, id string) (domain.Monitor, error)
	List(ctx context.Context) ([]domain.Monitor, error)
	Update(ctx context.Context, m domain.Monitor) (domain.Monitor, error)
	Delete(ctx context.Context, id string) error
}

const monitorsPath = "/api/v1/monitors"

func (h *handlers) createMonitor(w http.ResponseWriter, r *http.Request) {
	var req createMonitorRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	m := domain.Monitor{
		Name:            strings.TrimSpace(req.Name),
		URL:             strings.TrimSpace(req.URL),
		Method:          normalizeMethod(valueOr(req.Method, domain.DefaultMethod)),
		IntervalSeconds: valueOr(req.IntervalSeconds, domain.DefaultIntervalSeconds),
		TimeoutSeconds:  valueOr(req.TimeoutSeconds, domain.DefaultTimeoutSeconds),
		ExpectedStatus:  valueOr(req.ExpectedStatus, domain.DefaultExpectedStatus),
		Enabled:         valueOr(req.Enabled, true),
	}
	if !h.validate(w, m) {
		return
	}

	created, err := h.monitors.Create(r.Context(), m)
	if err != nil {
		h.writeServerError(w, r, err)
		return
	}
	w.Header().Set("Location", monitorsPath+"/"+created.ID)
	writeJSON(w, http.StatusCreated, toMonitorResponse(created))
}

func (h *handlers) listMonitors(w http.ResponseWriter, r *http.Request) {
	monitors, err := h.monitors.List(r.Context())
	if err != nil {
		h.writeServerError(w, r, err)
		return
	}
	resp := listMonitorsResponse{Monitors: make([]monitorResponse, 0, len(monitors))}
	for _, m := range monitors {
		resp.Monitors = append(resp.Monitors, toMonitorResponse(m))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) getMonitor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !domain.IsValidID(id) {
		writeNotFound(w)
		return
	}
	m, err := h.monitors.Get(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(m))
}

// updateMonitor applies a partial update: it loads the monitor, overlays the
// fields present in the request, validates the result as a whole and saves it.
// Pausing and resuming a monitor is just {"enabled": false} / {"enabled": true}.
func (h *handlers) updateMonitor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !domain.IsValidID(id) {
		writeNotFound(w)
		return
	}
	var req updateMonitorRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	m, err := h.monitors.Get(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}

	if req.Name != nil {
		m.Name = strings.TrimSpace(*req.Name)
	}
	if req.URL != nil {
		m.URL = strings.TrimSpace(*req.URL)
	}
	if req.Method != nil {
		m.Method = normalizeMethod(*req.Method)
	}
	m.IntervalSeconds = valueOr(req.IntervalSeconds, m.IntervalSeconds)
	m.TimeoutSeconds = valueOr(req.TimeoutSeconds, m.TimeoutSeconds)
	m.ExpectedStatus = valueOr(req.ExpectedStatus, m.ExpectedStatus)
	m.Enabled = valueOr(req.Enabled, m.Enabled)

	if !h.validate(w, m) {
		return
	}

	updated, err := h.monitors.Update(r.Context(), m)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(updated))
}

func (h *handlers) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !domain.IsValidID(id) {
		writeNotFound(w)
		return
	}
	if err := h.monitors.Delete(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validate runs domain validation. When it fails it has already written the
// 422 response and returns false.
func (h *handlers) validate(w http.ResponseWriter, m domain.Monitor) bool {
	err := m.Validate()
	if err == nil {
		return true
	}
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		writeValidation(w, ve)
		return false
	}
	// Validate only returns *ValidationError today; treat anything else as a bug.
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	return false
}

// writeStoreError maps "not found" to 404 and everything else to a logged 500.
func (h *handlers) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeNotFound(w)
		return
	}
	h.writeServerError(w, r, err)
}

func normalizeMethod(m string) string {
	return strings.ToUpper(strings.TrimSpace(m))
}
