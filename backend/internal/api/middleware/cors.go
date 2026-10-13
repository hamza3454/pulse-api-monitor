package middleware

import "net/http"

// CORS allows a browser app served from allowedOrigin (for example the Vite
// dev server) to call the API. Only that exact origin is allowed; requests from
// other origins get no CORS headers, so the browser blocks them. An empty
// allowedOrigin disables CORS entirely.
func CORS(allowedOrigin string) Middleware {
	return func(next http.Handler) http.Handler {
		if allowedOrigin == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == allowedOrigin {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")

				// Answer the browser's preflight check directly.
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
