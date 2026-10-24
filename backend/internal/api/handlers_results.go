package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"pulse/internal/domain"
)

// ResultStore is what the handlers need to read check history. It is defined
// here, where it is used, like MonitorStore.
type ResultStore interface {
	List(ctx context.Context, monitorID string, limit int, before *time.Time) ([]domain.CheckResult, error)
}

const (
	defaultResultsLimit = 50
	maxResultsLimit     = 500
)

// listResults serves GET /api/v1/monitors/{id}/results?limit=N&before=T.
//
// Results are newest first. Paging works with a cursor rather than an offset:
// to get the next page, pass the previous response's next_before as ?before=.
// That stays fast on a table that grows constantly, and new checks arriving
// between requests never shift or repeat rows.
func (h *handlers) listResults(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !domain.IsValidID(id) {
		writeNotFound(w)
		return
	}

	limit, before, problem := parseResultsQuery(r)
	if problem != "" {
		writeError(w, http.StatusBadRequest, "invalid_query", problem)
		return
	}

	// A monitor with no results yet is an empty list; a monitor that does not
	// exist is a 404. Only the monitors table can tell them apart.
	if _, err := h.monitors.Get(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}

	results, err := h.results.List(r.Context(), id, limit, before)
	if err != nil {
		h.writeServerError(w, r, err)
		return
	}

	resp := listResultsResponse{Results: make([]resultResponse, 0, len(results))}
	for _, res := range results {
		resp.Results = append(resp.Results, toResultResponse(res))
	}
	// A full page means there may be more behind it.
	if len(results) == limit {
		resp.NextBefore = &results[len(results)-1].CheckedAt
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseResultsQuery reads ?limit= and ?before=. problem is non-empty when a
// parameter is present but unusable.
func parseResultsQuery(r *http.Request) (limit int, before *time.Time, problem string) {
	limit = defaultResultsLimit
	q := r.URL.Query()

	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxResultsLimit {
			return 0, nil, "limit must be a whole number between 1 and " + strconv.Itoa(maxResultsLimit)
		}
		limit = n
	}
	if raw := q.Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return 0, nil, "before must be an RFC 3339 timestamp, e.g. 2026-10-24T15:04:05Z"
		}
		before = &t
	}
	return limit, before, ""
}
