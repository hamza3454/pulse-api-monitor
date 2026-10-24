package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"pulse/internal/domain"
)

// fakeResults is an in-memory ResultStore that records the last query it saw.
type fakeResults struct {
	rows       []domain.CheckResult // newest first, as the real store returns them
	err        error
	gotLimit   int
	gotBefore  *time.Time
	gotMonitor string
}

func (f *fakeResults) List(_ context.Context, monitorID string, limit int, before *time.Time) ([]domain.CheckResult, error) {
	f.gotMonitor, f.gotLimit, f.gotBefore = monitorID, limit, before
	if f.err != nil {
		return nil, f.err
	}
	var out []domain.CheckResult
	for _, r := range f.rows {
		if r.MonitorID != monitorID || (before != nil && !r.CheckedAt.Before(*before)) {
			continue
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func intp(n int) *int       { return &n }
func strp(s string) *string { return &s }

func resultsRouter(monitors MonitorStore, results ResultStore) http.Handler {
	return NewRouter(Deps{Monitors: monitors, Results: results, Logger: quietTestLogger()})
}

func TestListResults(t *testing.T) {
	store := newFakeStore()
	created, _ := store.Create(context.Background(), domain.Monitor{Name: "m", URL: "https://a.io"})

	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	results := &fakeResults{rows: []domain.CheckResult{
		{ID: 3, MonitorID: created.ID, CheckedAt: base.Add(2 * time.Minute), Success: false, Error: strp("request timed out after 10s")},
		{ID: 2, MonitorID: created.ID, CheckedAt: base.Add(time.Minute), Success: true, StatusCode: intp(200), LatencyMS: intp(87)},
		{ID: 1, MonitorID: created.ID, CheckedAt: base, Success: true, StatusCode: intp(200), LatencyMS: intp(91)},
	}}
	h := resultsRouter(store, results)

	rec := do(t, h, http.MethodGet, monitorsPath+"/"+created.ID+"/results", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
	}
	got := decode[listResultsResponse](t, rec)
	if len(got.Results) != 3 || got.Results[0].ID != 3 || got.Results[2].ID != 1 {
		t.Fatalf("results = %+v, want ids 3,2,1 (newest first)", got.Results)
	}
	if got.NextBefore != nil {
		t.Errorf("next_before = %v, want null when fewer results than the limit were returned", got.NextBefore)
	}

	// Nullable fields serialize as JSON null, not as zero values.
	body := rec.Body.String()
	for _, want := range []string{`"status_code":null`, `"latency_ms":null`, `"error":null`, `"error":"request timed out after 10s"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s:\n%s", want, body)
		}
	}
	if results.gotLimit != defaultResultsLimit || results.gotBefore != nil {
		t.Errorf("store saw limit=%d before=%v, want the default limit and no cursor", results.gotLimit, results.gotBefore)
	}
}

func TestListResultsEmptyIsAnEmptyArray(t *testing.T) {
	store := newFakeStore()
	created, _ := store.Create(context.Background(), domain.Monitor{Name: "m"})
	h := resultsRouter(store, &fakeResults{})

	rec := do(t, h, http.MethodGet, monitorsPath+"/"+created.ID+"/results", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"results":[]`) {
		t.Errorf("status %d body %s, want 200 and an empty array (not null)", rec.Code, rec.Body)
	}
}

func TestListResultsPaging(t *testing.T) {
	store := newFakeStore()
	created, _ := store.Create(context.Background(), domain.Monitor{Name: "m"})
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	var rows []domain.CheckResult
	for i := 5; i >= 1; i-- { // newest first
		rows = append(rows, domain.CheckResult{ID: int64(i), MonitorID: created.ID, CheckedAt: base.Add(time.Duration(i) * time.Minute), Success: true})
	}
	h := resultsRouter(store, &fakeResults{rows: rows})
	path := monitorsPath + "/" + created.ID + "/results"

	page1 := decode[listResultsResponse](t, do(t, h, http.MethodGet, path+"?limit=2", ""))
	if len(page1.Results) != 2 || page1.NextBefore == nil {
		t.Fatalf("page 1 = %+v, want 2 results and a next_before cursor", page1)
	}

	next := path + "?limit=2&before=" + url.QueryEscape(page1.NextBefore.Format(time.RFC3339Nano))
	page2 := decode[listResultsResponse](t, do(t, h, http.MethodGet, next, ""))
	if len(page2.Results) != 2 || page2.Results[0].ID != 3 || page2.Results[1].ID != 2 {
		t.Fatalf("page 2 = %+v, want ids 3 and 2 (continuing where page 1 stopped)", page2.Results)
	}

	next = path + "?limit=2&before=" + url.QueryEscape(page2.NextBefore.Format(time.RFC3339Nano))
	page3 := decode[listResultsResponse](t, do(t, h, http.MethodGet, next, ""))
	if len(page3.Results) != 1 || page3.NextBefore != nil {
		t.Errorf("page 3 = %+v, want the last result and no further cursor", page3)
	}
}

func TestListResultsRejectsBadQueries(t *testing.T) {
	store := newFakeStore()
	created, _ := store.Create(context.Background(), domain.Monitor{Name: "m"})
	h := resultsRouter(store, &fakeResults{})
	path := monitorsPath + "/" + created.ID + "/results"

	for _, q := range []string{"limit=0", "limit=-3", "limit=abc", "limit=501", "limit=1.5", "before=yesterday", "before=2026-10-24"} {
		rec := do(t, h, http.MethodGet, path+"?"+q, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("?%s status = %d, want 400", q, rec.Code)
			continue
		}
		if decode[errorBody](t, rec).Error.Code != "invalid_query" {
			t.Errorf("?%s code is not invalid_query: %s", q, rec.Body)
		}
	}

	if rec := do(t, h, http.MethodGet, path+"?limit=500", ""); rec.Code != http.StatusOK {
		t.Errorf("limit=500 (the maximum) status = %d, want 200", rec.Code)
	}
}

func TestListResultsUnknownMonitorIs404(t *testing.T) {
	h := resultsRouter(newFakeStore(), &fakeResults{})
	for _, id := range []string{"3f2504e0-4f89-41d3-9a0c-0305e82c3301", "not-a-uuid"} {
		rec := do(t, h, http.MethodGet, monitorsPath+"/"+id+"/results", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("monitor %q: status = %d, want 404", id, rec.Code)
		}
	}
}

func TestListResultsStoreFailureIsGeneric500(t *testing.T) {
	store := newFakeStore()
	created, _ := store.Create(context.Background(), domain.Monitor{Name: "m"})
	h := resultsRouter(store, &fakeResults{err: errors.New(`relation "secret_table" does not exist`)})

	rec := do(t, h, http.MethodGet, monitorsPath+"/"+created.ID+"/results", "")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret_table") {
		t.Errorf("status %d body %s, want a generic 500 that leaks nothing", rec.Code, rec.Body)
	}
}
