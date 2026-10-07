// Package api contains Pulse's REST API: routing, handlers and middleware.
package api

import "net/http"

// NewRouter builds the HTTP handler for the API service. Routes use the Go
// 1.22+ standard-library mux ("METHOD /path"), so no router dependency is
// needed.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	return mux
}
