// Package middleware holds the HTTP middleware shared by Pulse services:
// request IDs, access logging, panic recovery and CORS.
package middleware

import "net/http"

// Middleware wraps a handler with extra behaviour.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first one listed is the outermost
// (runs first on the way in, last on the way out).
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
