package worker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"pulse/internal/domain"
)

func monitorFor(url string) domain.Monitor {
	return domain.Monitor{
		ID:              "3f2504e0-4f89-41d3-9a0c-0305e82c3301",
		Name:            "test",
		URL:             url,
		Method:          "GET",
		IntervalSeconds: 60,
		TimeoutSeconds:  5,
		ExpectedStatus:  200,
		Enabled:         true,
	}
}

func TestCheckSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m := monitorFor(srv.URL)
	r := NewChecker().Check(context.Background(), m)

	if !r.Success || r.Error != nil {
		t.Fatalf("result = %+v, want success with no error", r)
	}
	if r.StatusCode == nil || *r.StatusCode != 200 {
		t.Errorf("StatusCode = %v, want 200", r.StatusCode)
	}
	if r.LatencyMS == nil || *r.LatencyMS < 50 || *r.LatencyMS > 2000 {
		t.Errorf("LatencyMS = %v, want at least the server's 50ms delay", r.LatencyMS)
	}
	if r.MonitorID != m.ID || !domain.IsValidID(r.JobID) {
		t.Errorf("identifiers wrong: monitor %q job %q", r.MonitorID, r.JobID)
	}
	if time.Since(r.CheckedAt) > time.Minute || r.CheckedAt.Location() != time.UTC {
		t.Errorf("CheckedAt = %v, want a recent UTC time", r.CheckedAt)
	}
}

func TestCheckEachCallGetsItsOwnJobID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	c := NewChecker()
	a := c.Check(context.Background(), monitorFor(srv.URL))
	b := c.Check(context.Background(), monitorFor(srv.URL))
	if a.JobID == b.JobID {
		t.Errorf("two checks shared job ID %s", a.JobID)
	}
}

func TestCheckWrongStatusIsFailureButKeepsStatusAndLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewChecker().Check(context.Background(), monitorFor(srv.URL))

	if r.Success {
		t.Fatal("a 500 must not count as success when 200 is expected")
	}
	if r.Error == nil || *r.Error != "expected status 200, got 500" {
		t.Errorf("Error = %v, want %q", r.Error, "expected status 200, got 500")
	}
	if r.StatusCode == nil || *r.StatusCode != 500 || r.LatencyMS == nil {
		t.Errorf("a response did arrive, so status and latency should be set: %+v", r)
	}
}

func TestCheckCustomExpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := monitorFor(srv.URL)
	m.ExpectedStatus = 204
	if r := NewChecker().Check(context.Background(), m); !r.Success {
		t.Errorf("204 expected and received, got %+v", r)
	}
}

func TestCheckTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(release)

	m := monitorFor(srv.URL)
	m.TimeoutSeconds = 1

	start := time.Now()
	r := NewChecker().Check(context.Background(), m)
	elapsed := time.Since(start)

	if r.Success {
		t.Fatal("a hung server must fail the check")
	}
	if r.Error == nil || *r.Error != "request timed out after 1s" {
		t.Errorf("Error = %v, want %q", r.Error, "request timed out after 1s")
	}
	if r.StatusCode != nil || r.LatencyMS != nil {
		t.Errorf("nothing came back, so status and latency must be nil: %+v", r)
	}
	if elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("check took %v, want about the 1s timeout", elapsed)
	}
}

func TestCheckConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close() // nothing is listening on addr any more

	r := NewChecker().Check(context.Background(), monitorFor(addr))

	if r.Success || r.Error == nil || *r.Error != "connection refused" {
		t.Errorf("result = %+v (error %v), want failure %q", r, r.Error, "connection refused")
	}
}

func TestCheckUsesConfiguredMethodAndUserAgent(t *testing.T) {
	var method, agent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method.Store(r.Method)
		agent.Store(r.UserAgent())
	}))
	defer srv.Close()

	m := monitorFor(srv.URL)
	m.Method = "HEAD"
	if r := NewChecker().Check(context.Background(), m); !r.Success {
		t.Fatalf("HEAD check failed: %+v", r)
	}
	if method.Load() != "HEAD" {
		t.Errorf("server saw method %v, want HEAD", method.Load())
	}
	if agent.Load() != userAgent {
		t.Errorf("server saw User-Agent %v, want %q", agent.Load(), userAgent)
	}
}

func TestCheckFollowsRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/new", func(http.ResponseWriter, *http.Request) {})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if r := NewChecker().Check(context.Background(), monitorFor(srv.URL+"/old")); !r.Success {
		t.Errorf("redirect to a healthy page should pass, got %+v", r)
	}
}

func TestCheckRedirectLoopFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	r := NewChecker().Check(context.Background(), monitorFor(srv.URL))
	if r.Success || r.Error == nil || !strings.Contains(*r.Error, "redirects") {
		t.Errorf("result = %+v (error %v), want a failure mentioning redirects", r, r.Error)
	}
}

func TestCheckHugeBodyDoesNotHangOrFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 64<<10)
		for range 200 { // ~12.5 MiB, far above the 1 MiB read cap
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	start := time.Now()
	r := NewChecker().Check(context.Background(), monitorFor(srv.URL))
	if !r.Success {
		t.Errorf("large response with the right status should pass, got %+v", r)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("check took %v; the body read should be capped", time.Since(start))
	}
}

func TestCheckInvalidURL(t *testing.T) {
	r := NewChecker().Check(context.Background(), monitorFor("://bad"))
	if r.Success || r.Error == nil || !strings.HasPrefix(*r.Error, "invalid request") {
		t.Errorf("result = %+v (error %v), want an 'invalid request' failure", r, r.Error)
	}
}

func TestCheckCanceledByCaller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done(): // the client gave up
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	r := NewChecker().Check(ctx, monitorFor(srv.URL))

	if r.Error == nil || *r.Error != errCheckCanceled {
		t.Errorf("Error = %v, want %q", r.Error, errCheckCanceled)
	}
	if time.Since(start) > time.Second {
		t.Errorf("cancellation took %v to take effect", time.Since(start))
	}
}

func TestDescribeError(t *testing.T) {
	live := context.Background()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name   string
		parent context.Context
		err    error
		want   string
	}{
		{"deadline", live, context.DeadlineExceeded, "request timed out after 10s"},
		{"deadline inside url.Error", live, &url.Error{Op: "Get", URL: "http://x", Err: context.DeadlineExceeded}, "request timed out after 10s"},
		{"dns", live, &url.Error{Err: &net.DNSError{Name: "nope.example", Err: "no such host"}}, "DNS lookup failed for nope.example"},
		{"refused", live, &url.Error{Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, "connection refused"},
		{"canceled error", live, context.Canceled, errCheckCanceled},
		{"parent canceled wins", canceled, context.DeadlineExceeded, errCheckCanceled},
		{"unknown error passes through", live, errors.New("tls: bad certificate"), "tls: bad certificate"},
		{"long error truncated", live, errors.New(strings.Repeat("x", 1000)), strings.Repeat("x", maxErrorLength) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeError(tt.parent, tt.err, 10*time.Second); got != tt.want {
				t.Errorf("describeError() = %q, want %q", got, tt.want)
			}
		})
	}
}
