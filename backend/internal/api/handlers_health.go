package api

import (
	"encoding/json"
	"net/http"
)

// healthz is the liveness probe: it answers 200 as long as the process can
// serve HTTP. Kubernetes uses it (Phase 6) to decide whether to restart a pod.
// A readiness check that also verifies Postgres is added in Phase 1.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
