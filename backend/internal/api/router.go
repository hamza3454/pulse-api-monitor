// Package api contains Pulse's REST API: routing, handlers and middleware.
package api

import (
	"log/slog"
	"net/http"

	"pulse/internal/api/middleware"
)

// Deps are the collaborators the API needs. Passing them in (rather than using
// globals) keeps handlers easy to test with fakes.
type Deps struct {
	Monitors   MonitorStore
	Results    ResultStore
	DB         Pinger // checked by /readyz; may be nil
	Logger     *slog.Logger
	CORSOrigin string // browser origin allowed to call the API; empty disables CORS
}

type handlers struct {
	monitors MonitorStore
	results  ResultStore
	db       Pinger
	logger   *slog.Logger
}

// NewRouter builds the HTTP handler for the API service. Routes use the Go
// 1.22+ standard-library mux ("METHOD /path/{param}"), so no router dependency
// is needed.
func NewRouter(d Deps) http.Handler {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &handlers{monitors: d.Monitors, results: d.Results, db: d.DB, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	mux.HandleFunc("POST "+monitorsPath, h.createMonitor)
	mux.HandleFunc("GET "+monitorsPath, h.listMonitors)
	mux.HandleFunc("GET "+monitorsPath+"/{id}", h.getMonitor)
	mux.HandleFunc("PATCH "+monitorsPath+"/{id}", h.updateMonitor)
	mux.HandleFunc("DELETE "+monitorsPath+"/{id}", h.deleteMonitor)
	mux.HandleFunc("GET "+monitorsPath+"/{id}/results", h.listResults)

	// Outermost first: the request ID must exist before anything logs.
	return middleware.Chain(mux,
		middleware.RequestID,
		middleware.Recover(logger),
		middleware.Logging(logger),
		middleware.CORS(d.CORSOrigin),
	)
}
