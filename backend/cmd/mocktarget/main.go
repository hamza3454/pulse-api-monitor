// Command mocktarget is a tiny fake API for trying Pulse out. Point monitors at
// it and break it on purpose:
//
//	GET  /ok               always 200
//	GET  /slow?ms=2000     waits that long (max 30s), then 200
//	GET  /status/503       answers with the given status code (200-599)
//	GET  /flaky?fail=0.3   fails with 500 that fraction of the time (default 0.3)
//	GET  /switch           200 while the switch is up, 503 while it is down
//	POST /switch/down      flip the switch down: start an "outage"
//	POST /switch/up        flip it back up: "recover"
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	maxSlow      = 30 * time.Second
	defaultFlaky = 0.3
)

func main() {
	addr := ":9000"
	if v := os.Getenv("MOCK_ADDR"); v != "" {
		addr = v
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(rand.Float64),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("mock target listening on %s (try: curl localhost%s/)", addr, addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// newHandler builds the mock API. randFloat returns a number in [0, 1); it is a
// parameter so tests can make /flaky deterministic.
func newHandler(randFloat func() float64) http.Handler {
	var switchDown atomic.Bool
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		write(w, `Pulse mock target

  GET  /ok               always 200
  GET  /slow?ms=2000     waits that long (max 30s), then 200
  GET  /status/503       answers with the given status code
  GET  /flaky?fail=0.3   fails with 500 that fraction of the time
  GET  /switch           200 while up, 503 while down
  POST /switch/down      start an outage
  POST /switch/up        end it
`)
	})

	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) {
		write(w, "ok\n")
	})

	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(parseSlow(r.URL.Query().Get("ms"))):
			write(w, "ok (slowly)\n")
		case <-r.Context().Done(): // the caller gave up
		}
	})

	mux.HandleFunc("GET /status/{code}", func(w http.ResponseWriter, r *http.Request) {
		code, err := strconv.Atoi(r.PathValue("code"))
		if err != nil || code < 200 || code > 599 {
			http.Error(w, "status must be a number from 200 to 599", http.StatusBadRequest)
			return
		}
		w.WriteHeader(code)
		write(w, fmt.Sprintf("status %d\n", code))
	})

	mux.HandleFunc("GET /flaky", func(w http.ResponseWriter, r *http.Request) {
		if randFloat() < parseFailRate(r.URL.Query().Get("fail")) {
			http.Error(w, "flaky failure", http.StatusInternalServerError)
			return
		}
		write(w, "ok\n")
	})

	mux.HandleFunc("GET /switch", func(w http.ResponseWriter, _ *http.Request) {
		if switchDown.Load() {
			http.Error(w, "switch is down", http.StatusServiceUnavailable)
			return
		}
		write(w, "switch is up\n")
	})
	mux.HandleFunc("POST /switch/down", func(w http.ResponseWriter, _ *http.Request) {
		switchDown.Store(true)
		write(w, "switch is now down: GET /switch returns 503\n")
	})
	mux.HandleFunc("POST /switch/up", func(w http.ResponseWriter, _ *http.Request) {
		switchDown.Store(false)
		write(w, "switch is now up: GET /switch returns 200\n")
	})

	return mux
}

// parseSlow turns ?ms= into a delay: missing or invalid means 1s, and it is
// capped so a typo cannot hold a connection for hours.
func parseSlow(raw string) time.Duration {
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		return time.Second
	}
	return min(time.Duration(ms)*time.Millisecond, maxSlow)
}

// parseFailRate turns ?fail= into a probability in [0, 1].
func parseFailRate(raw string) float64 {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f < 0 || f > 1 {
		return defaultFlaky
	}
	return f
}

// write sends text to the caller. If the caller has already hung up there is
// nobody to tell, so a write error is deliberately ignored.
func write(w http.ResponseWriter, text string) {
	_, _ = io.WriteString(w, text)
}
