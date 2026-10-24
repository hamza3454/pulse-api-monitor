package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestOKAndIndex(t *testing.T) {
	h := newHandler(func() float64 { return 0 })
	if rec := get(h, http.MethodGet, "/ok"); rec.Code != http.StatusOK {
		t.Errorf("/ok status = %d, want 200", rec.Code)
	}
	rec := get(h, http.MethodGet, "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/switch/down") {
		t.Errorf("index status %d body %q, want 200 listing the endpoints", rec.Code, rec.Body)
	}
	if rec := get(h, http.MethodGet, "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", rec.Code)
	}
}

func TestStatus(t *testing.T) {
	h := newHandler(func() float64 { return 0 })
	for path, want := range map[string]int{
		"/status/200": 200, "/status/204": 204, "/status/404": 404, "/status/503": 503,
		"/status/abc": 400, "/status/99": 400, "/status/600": 400,
	} {
		if got := get(h, http.MethodGet, path).Code; got != want {
			t.Errorf("%s status = %d, want %d", path, got, want)
		}
	}
}

func TestFlakyUsesTheFailRate(t *testing.T) {
	roll := 0.5
	h := newHandler(func() float64 { return roll })

	// A roll of 0.5 fails when the fail rate is above 0.5 and passes otherwise.
	if got := get(h, http.MethodGet, "/flaky?fail=0.9").Code; got != http.StatusInternalServerError {
		t.Errorf("roll 0.5 with fail=0.9: status = %d, want 500", got)
	}
	if got := get(h, http.MethodGet, "/flaky?fail=0.1").Code; got != http.StatusOK {
		t.Errorf("roll 0.5 with fail=0.1: status = %d, want 200", got)
	}
	if got := get(h, http.MethodGet, "/flaky?fail=1").Code; got != http.StatusInternalServerError {
		t.Errorf("fail=1 must always fail, got %d", got)
	}
	if got := get(h, http.MethodGet, "/flaky?fail=0").Code; got != http.StatusOK {
		t.Errorf("fail=0 must never fail, got %d", got)
	}
	// Default rate is 0.3: a 0.5 roll passes, a 0.1 roll fails.
	if got := get(h, http.MethodGet, "/flaky").Code; got != http.StatusOK {
		t.Errorf("default rate with roll 0.5: status = %d, want 200", got)
	}
	roll = 0.1
	if got := get(h, http.MethodGet, "/flaky").Code; got != http.StatusInternalServerError {
		t.Errorf("default rate with roll 0.1: status = %d, want 500", got)
	}
}

func TestSwitchStartsUpCanGoDownAndUp(t *testing.T) {
	h := newHandler(func() float64 { return 0 })

	if got := get(h, http.MethodGet, "/switch").Code; got != http.StatusOK {
		t.Fatalf("switch should start up, got %d", got)
	}
	if got := get(h, http.MethodPost, "/switch/down").Code; got != http.StatusOK {
		t.Fatalf("POST /switch/down status = %d", got)
	}
	if got := get(h, http.MethodGet, "/switch").Code; got != http.StatusServiceUnavailable {
		t.Errorf("after going down, status = %d, want 503", got)
	}
	if got := get(h, http.MethodPost, "/switch/up").Code; got != http.StatusOK {
		t.Fatalf("POST /switch/up status = %d", got)
	}
	if got := get(h, http.MethodGet, "/switch").Code; got != http.StatusOK {
		t.Errorf("after coming back up, status = %d, want 200", got)
	}
}

func TestSwitchStateIsPerHandler(t *testing.T) {
	a := newHandler(func() float64 { return 0 })
	b := newHandler(func() float64 { return 0 })
	get(a, http.MethodPost, "/switch/down")
	if got := get(b, http.MethodGet, "/switch").Code; got != http.StatusOK {
		t.Errorf("a second server shares state with the first (status %d)", got)
	}
}

func TestSwitchCannotBeFlippedWithGet(t *testing.T) {
	h := newHandler(func() float64 { return 0 })
	if got := get(h, http.MethodGet, "/switch/down").Code; got == http.StatusOK {
		t.Error("GET /switch/down must not flip the switch (so crawlers and previews cannot cause outages)")
	}
	if got := get(h, http.MethodGet, "/switch").Code; got != http.StatusOK {
		t.Errorf("switch changed by a GET: status = %d", got)
	}
}

func TestSlowWaitsAndGivesUpWhenCallerDoes(t *testing.T) {
	h := newHandler(func() float64 { return 0 })

	start := time.Now()
	rec := get(h, http.MethodGet, "/slow?ms=80")
	if rec.Code != http.StatusOK || time.Since(start) < 80*time.Millisecond {
		t.Errorf("status %d after %v, want 200 after at least 80ms", rec.Code, time.Since(start))
	}
}

func TestParsers(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"":       time.Second,
		"abc":    time.Second,
		"-5":     time.Second,
		"0":      0,
		"250":    250 * time.Millisecond,
		"999999": maxSlow,
		"30000":  30 * time.Second,
		"30001":  maxSlow,
	} {
		if got := parseSlow(raw); got != want {
			t.Errorf("parseSlow(%q) = %v, want %v", raw, got, want)
		}
	}
	for raw, want := range map[string]float64{
		"": defaultFlaky, "x": defaultFlaky, "-0.1": defaultFlaky, "1.5": defaultFlaky,
		"0": 0, "0.25": 0.25, "1": 1,
	} {
		if got := parseFailRate(raw); got != want {
			t.Errorf("parseFailRate(%q) = %v, want %v", raw, got, want)
		}
	}
}
