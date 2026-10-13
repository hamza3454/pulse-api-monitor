package api

import (
	"context"
	"net/http"
	"time"
)

// Pinger is anything that can report whether it is reachable; the Postgres
// connection pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

const readyTimeout = 2 * time.Second

// healthz is the liveness probe: it answers 200 as long as the process can
// serve HTTP. Kubernetes uses it (Phase 6) to decide whether to restart a pod.
// It deliberately does not check dependencies: restarting the API cannot fix a
// database outage.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz is the readiness probe: 200 only when the database answers, 503
// otherwise. Kubernetes uses it to stop routing traffic to an instance that
// cannot do useful work.
func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	if h.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := h.db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
