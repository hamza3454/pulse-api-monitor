// Package worker runs health checks: a Checker that performs one HTTP check,
// and a Pool that runs many checks concurrently on a fixed set of goroutines.
package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"pulse/internal/domain"
)

const (
	userAgent    = "Pulse-Monitor/1.0"
	maxRedirects = 5
	// maxBodyBytes caps how much of a response is read. Latency is measured
	// until the body has been read, but a huge or endless body must not tie up
	// a worker.
	maxBodyBytes     = 1 << 20 // 1 MiB
	maxErrorLength   = 300
	errCheckCanceled = "check canceled"
)

// Checker performs a single HTTP health check. It is safe for concurrent use,
// and its connection pool is shared by all workers.
type Checker struct {
	client *http.Client
}

// NewChecker returns a Checker with sensible transport limits. The per-check
// timeout is not set here; each monitor supplies its own.
func NewChecker() *Checker {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Checker{client: &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}}
}

// Check requests the monitor's URL and reports what happened. It always
// returns a result: failures (timeouts, refused connections, wrong status) are
// outcomes to record, not errors to propagate.
//
// A check passes when the response status equals the monitor's expected
// status. The reported latency covers the whole response (headers and body up
// to a size cap). When nothing came back at all, StatusCode and LatencyMS are
// nil.
//
// If ctx itself is canceled (for example during shutdown) the result is marked
// as a failure with the error "check canceled"; callers should discard such
// results rather than record a fake outage.
func (c *Checker) Check(ctx context.Context, m domain.Monitor) domain.CheckResult {
	start := time.Now()
	result := domain.CheckResult{
		MonitorID: m.ID,
		JobID:     domain.NewID(),
		CheckedAt: start.UTC(),
	}
	fail := func(msg string) domain.CheckResult {
		result.Success = false
		result.Error = &msg
		return result
	}

	timeout := time.Duration(m.TimeoutSeconds) * time.Second
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, m.Method, m.URL, nil)
	if err != nil {
		return fail("invalid request: " + truncate(err.Error()))
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return fail(describeError(ctx, err, timeout))
	}
	defer func() { _ = resp.Body.Close() }()

	// Read (and throw away) the body so the latency reflects the full response.
	_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
	latency := int(time.Since(start).Milliseconds())
	status := resp.StatusCode
	result.StatusCode = &status
	result.LatencyMS = &latency

	if readErr != nil {
		return fail("error reading response: " + describeError(ctx, readErr, timeout))
	}
	if status != m.ExpectedStatus {
		return fail(fmt.Sprintf("expected status %d, got %d", m.ExpectedStatus, status))
	}
	result.Success = true
	return result
}

// describeError turns a low-level network error into a short message a person
// can act on. parent is the caller's context, used to tell "the check timed
// out" apart from "the whole process is shutting down".
func describeError(parent context.Context, err error, timeout time.Duration) string {
	if parent.Err() != nil {
		return errCheckCanceled
	}

	// The URL is already known to the reader; drop the "Get \"...\":" wrapper.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}

	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("request timed out after %s", timeout)
	case errors.Is(err, context.Canceled):
		return errCheckCanceled
	case errors.As(err, &dnsErr):
		return "DNS lookup failed for " + dnsErr.Name
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	default:
		return truncate(err.Error())
	}
}

func truncate(s string) string {
	if len(s) <= maxErrorLength {
		return s
	}
	return s[:maxErrorLength] + "..."
}
