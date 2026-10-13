package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestChainOrder(t *testing.T) {
	var order []string
	mk := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mk("first"), mk("second"))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got := strings.Join(order, ","); got != "first,second,handler" {
		t.Errorf("order = %s, want first,second,handler", got)
	}
}

func TestRequestIDGeneratedAndStored(t *testing.T) {
	var inContext string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		inContext = RequestIDFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	header := rec.Header().Get("X-Request-ID")
	if header == "" || header != inContext {
		t.Errorf("header = %q, context = %q; want the same non-empty ID", header, inContext)
	}
}

func TestRequestIDKeepsValidIncomingID(t *testing.T) {
	h := RequestID(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc-123_XYZ")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "abc-123_XYZ" {
		t.Errorf("ID = %q, want the incoming one kept", got)
	}
}

func TestRequestIDReplacesSuspiciousIncomingID(t *testing.T) {
	for _, bad := range []string{"has space", "semi;colon", strings.Repeat("a", 65), "new\tline"} {
		h := RequestID(okHandler())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", bad)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if got := rec.Header().Get("X-Request-ID"); got == bad || got == "" {
			t.Errorf("incoming %q -> %q, want a freshly generated ID", bad, got)
		}
	}
}

func TestRecoverReturns500AndLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("body = %s, want the standard error shape", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("panic value leaked to the client")
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("panic not logged: %s", logs.String())
	}
}

func TestLoggingRecordsStatusAndLevel(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	h := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	out := logs.String()
	for _, want := range []string{"method=GET", "path=/missing", "status=404", "level=WARN"} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q missing %q", out, want)
		}
	}
}

func TestCORSAllowedOrigin(t *testing.T) {
	h := CORS("http://localhost:5173")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q, want the allowed origin", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	called := false
	h := CORS("http://localhost:5173")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/monitors", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if called {
		t.Error("preflight should be answered by the middleware, not passed on")
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Errorf("Allow-Methods = %q, want PATCH included", rec.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestCORSOtherOriginGetsNoHeaders(t *testing.T) {
	h := CORS("http://localhost:5173")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q for a foreign origin, want none", got)
	}
}

func TestCORSDisabledWhenEmpty(t *testing.T) {
	h := CORS("")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q with CORS disabled, want none", got)
	}
}
