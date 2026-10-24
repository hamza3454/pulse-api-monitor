package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pulse/internal/domain"
)

// fakeStore is an in-memory MonitorStore. If err is set, every call fails.
type fakeStore struct {
	mu       sync.Mutex
	monitors map[string]domain.Monitor
	order    []string
	next     int
	err      error
}

func newFakeStore() *fakeStore {
	return &fakeStore{monitors: map[string]domain.Monitor{}}
}

func (f *fakeStore) Create(_ context.Context, m domain.Monitor) (domain.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Monitor{}, f.err
	}
	f.next++
	m.ID = fmt.Sprintf("00000000-0000-0000-0000-%012d", f.next)
	now := time.Now()
	m.NextCheckAt, m.CreatedAt, m.UpdatedAt = now, now, now
	f.monitors[m.ID] = m
	f.order = append([]string{m.ID}, f.order...) // newest first
	return m, nil
}

func (f *fakeStore) Get(_ context.Context, id string) (domain.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Monitor{}, f.err
	}
	m, ok := f.monitors[id]
	if !ok {
		return domain.Monitor{}, domain.ErrNotFound
	}
	return m, nil
}

func (f *fakeStore) List(context.Context) ([]domain.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := []domain.Monitor{}
	for _, id := range f.order {
		out = append(out, f.monitors[id])
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, m domain.Monitor) (domain.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Monitor{}, f.err
	}
	if _, ok := f.monitors[m.ID]; !ok {
		return domain.Monitor{}, domain.ErrNotFound
	}
	m.UpdatedAt = time.Now()
	f.monitors[m.ID] = m
	return m, nil
}

func (f *fakeStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if _, ok := f.monitors[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.monitors, id)
	for i, o := range f.order {
		if o == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func quietTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestRouter(store MonitorStore) http.Handler {
	return NewRouter(Deps{Monitors: store, Logger: quietTestLogger()})
}

// do sends a request with an optional JSON body and returns the recorder.
func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, rdr))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func createOK(t *testing.T, h http.Handler, body string) monitorResponse {
	t.Helper()
	rec := do(t, h, http.MethodPost, monitorsPath, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body)
	}
	return decode[monitorResponse](t, rec)
}

func TestCreateMonitorAppliesDefaults(t *testing.T) {
	h := newTestRouter(newFakeStore())
	rec := do(t, h, http.MethodPost, monitorsPath, `{"name":"  My API ","url":" https://api.example.com/health "}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got := decode[monitorResponse](t, rec)
	if got.Name != "My API" || got.URL != "https://api.example.com/health" {
		t.Errorf("name/url not trimmed: %+v", got)
	}
	if got.Method != "GET" || got.IntervalSeconds != 60 || got.TimeoutSeconds != 10 || got.ExpectedStatus != 200 || !got.Enabled {
		t.Errorf("defaults not applied: %+v", got)
	}
	if want := monitorsPath + "/" + got.ID; rec.Header().Get("Location") != want {
		t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
	}
}

func TestCreateMonitorWithExplicitValues(t *testing.T) {
	h := newTestRouter(newFakeStore())
	got := createOK(t, h, `{"name":"x","url":"http://localhost:9000/ok","method":"head","interval_seconds":30,
		"timeout_seconds":5,"expected_status":204,"enabled":false}`)

	if got.Method != "HEAD" || got.IntervalSeconds != 30 || got.TimeoutSeconds != 5 || got.ExpectedStatus != 204 || got.Enabled {
		t.Errorf("explicit values not kept: %+v", got)
	}
}

func TestCreateMonitorValidation(t *testing.T) {
	h := newTestRouter(newFakeStore())
	rec := do(t, h, http.MethodPost, monitorsPath, `{"name":"","url":"ftp://x","interval_seconds":1}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
	}
	body := decode[errorBody](t, rec)
	if body.Error.Code != "validation_failed" {
		t.Errorf("code = %q, want validation_failed", body.Error.Code)
	}
	if len(body.Error.Details) < 3 {
		t.Errorf("details = %v, want a line per problem (name, url, interval)", body.Error.Details)
	}
}

func TestCreateMonitorBadBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"not json", `hello`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"unknown field", `{"name":"x","url":"https://a.io","colour":"red"}`, http.StatusBadRequest},
		{"wrong type", `{"name":"x","url":"https://a.io","interval_seconds":"soon"}`, http.StatusBadRequest},
		{"trailing data", `{"name":"x","url":"https://a.io"} {"name":"y"}`, http.StatusBadRequest},
		{"array not object", `[]`, http.StatusBadRequest},
		{"too large", `{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, newTestRouter(newFakeStore()), http.MethodPost, monitorsPath, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body)
			}
			if decode[errorBody](t, rec).Error.Code == "" {
				t.Error("error response has no code")
			}
		})
	}
}

func TestListMonitors(t *testing.T) {
	h := newTestRouter(newFakeStore())

	empty := do(t, h, http.MethodGet, monitorsPath, "")
	if empty.Code != http.StatusOK {
		t.Fatalf("status = %d", empty.Code)
	}
	if !strings.Contains(empty.Body.String(), `"monitors":[]`) {
		t.Errorf("empty list body = %s, want an empty array (not null)", empty.Body)
	}

	createOK(t, h, `{"name":"first","url":"https://a.io"}`)
	createOK(t, h, `{"name":"second","url":"https://b.io"}`)

	list := decode[listMonitorsResponse](t, do(t, h, http.MethodGet, monitorsPath, ""))
	if len(list.Monitors) != 2 || list.Monitors[0].Name != "second" || list.Monitors[1].Name != "first" {
		t.Errorf("list = %+v, want [second first]", list.Monitors)
	}
}

func TestGetMonitor(t *testing.T) {
	h := newTestRouter(newFakeStore())
	created := createOK(t, h, `{"name":"x","url":"https://a.io"}`)

	rec := do(t, h, http.MethodGet, monitorsPath+"/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := decode[monitorResponse](t, rec); got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
}

func TestMonitorNotFound(t *testing.T) {
	h := newTestRouter(newFakeStore())
	missing := monitorsPath + "/3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	badID := monitorsPath + "/not-a-uuid"

	tests := []struct{ method, path, body string }{
		{http.MethodGet, missing, ""},
		{http.MethodGet, badID, ""},
		{http.MethodPatch, missing, `{"enabled":false}`},
		{http.MethodPatch, badID, `{"enabled":false}`},
		{http.MethodDelete, missing, ""},
		{http.MethodDelete, badID, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := do(t, h, tt.method, tt.path, tt.body)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body)
			}
			if got := decode[errorBody](t, rec).Error.Code; got != "not_found" {
				t.Errorf("code = %q, want not_found", got)
			}
		})
	}
}

func TestPatchChangesOnlySentFields(t *testing.T) {
	h := newTestRouter(newFakeStore())
	created := createOK(t, h, `{"name":"keep","url":"https://a.io","interval_seconds":120}`)

	rec := do(t, h, http.MethodPatch, monitorsPath+"/"+created.ID, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause status = %d; body = %s", rec.Code, rec.Body)
	}
	paused := decode[monitorResponse](t, rec)
	if paused.Enabled || paused.Name != "keep" || paused.IntervalSeconds != 120 || paused.URL != "https://a.io" {
		t.Errorf("pause changed more than enabled: %+v", paused)
	}

	rec = do(t, h, http.MethodPatch, monitorsPath+"/"+created.ID, `{"name":"renamed","interval_seconds":30,"enabled":true}`)
	resumed := decode[monitorResponse](t, rec)
	if !resumed.Enabled || resumed.Name != "renamed" || resumed.IntervalSeconds != 30 {
		t.Errorf("patch not applied: %+v", resumed)
	}
}

func TestPatchEmptyObjectChangesNothing(t *testing.T) {
	h := newTestRouter(newFakeStore())
	created := createOK(t, h, `{"name":"same","url":"https://a.io"}`)

	rec := do(t, h, http.MethodPatch, monitorsPath+"/"+created.ID, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := decode[monitorResponse](t, rec); got.Name != "same" || !got.Enabled {
		t.Errorf("empty patch changed the monitor: %+v", got)
	}
}

func TestPatchValidatesTheMergedResult(t *testing.T) {
	h := newTestRouter(newFakeStore())
	// Stored timeout is 10s; shrinking the interval to 10 is fine, but a
	// timeout above the interval is not, even though each value alone is legal.
	created := createOK(t, h, `{"name":"x","url":"https://a.io","interval_seconds":60,"timeout_seconds":30}`)

	rec := do(t, h, http.MethodPatch, monitorsPath+"/"+created.ID, `{"interval_seconds":20}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
	}

	// The failed patch must not have been saved.
	got := decode[monitorResponse](t, do(t, h, http.MethodGet, monitorsPath+"/"+created.ID, ""))
	if got.IntervalSeconds != 60 {
		t.Errorf("interval = %d after rejected patch, want 60", got.IntervalSeconds)
	}
}

func TestPatchRejectsUnknownFields(t *testing.T) {
	h := newTestRouter(newFakeStore())
	created := createOK(t, h, `{"name":"x","url":"https://a.io"}`)

	// "id" is read-only; silently ignoring it would hide client bugs.
	rec := do(t, h, http.MethodPatch, monitorsPath+"/"+created.ID, `{"id":"other"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDeleteMonitor(t *testing.T) {
	h := newTestRouter(newFakeStore())
	created := createOK(t, h, `{"name":"x","url":"https://a.io"}`)

	rec := do(t, h, http.MethodDelete, monitorsPath+"/"+created.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response has a body: %q", rec.Body)
	}
	if got := do(t, h, http.MethodGet, monitorsPath+"/"+created.ID, ""); got.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", got.Code)
	}
}

func TestStoreFailureReturnsGeneric500(t *testing.T) {
	store := newFakeStore()
	h := newTestRouter(store)
	created := createOK(t, h, `{"name":"x","url":"https://a.io"}`)
	store.err = errors.New(`password authentication failed for user "secret_user"`)

	for _, tt := range []struct{ method, path, body string }{
		{http.MethodPost, monitorsPath, `{"name":"y","url":"https://b.io"}`},
		{http.MethodGet, monitorsPath, ""},
		{http.MethodGet, monitorsPath + "/" + created.ID, ""},
		{http.MethodPatch, monitorsPath + "/" + created.ID, `{"enabled":false}`},
		{http.MethodDelete, monitorsPath + "/" + created.ID, ""},
	} {
		rec := do(t, h, tt.method, tt.path, tt.body)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s status = %d, want 500", tt.method, tt.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret_user") {
			t.Errorf("%s %s leaked internal error details: %s", tt.method, tt.path, rec.Body)
		}
		if decode[errorBody](t, rec).Error.Code != "internal_error" {
			t.Errorf("%s %s code is not internal_error", tt.method, tt.path)
		}
	}
}
