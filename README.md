# Pulse

A distributed API monitoring platform. Add an endpoint and Pulse checks it on a
schedule, tracking uptime, status codes, latency, failures and incidents.

> **Status: Phase 1 in progress.** Monitor CRUD REST API on PostgreSQL is done.
> Next: the in-process scheduler and goroutine worker pool, then the dashboard.

## Planned architecture

```
 React dashboard ──▶ API ──▶ PostgreSQL ◀────────────┐
                      │                              │
                      └──▶ Redis (latest status)     │
 Scheduler ──jobs──▶ Kafka ──▶ Workers (goroutine pools)
        Prometheus ◀── all services ──▶ Grafana
```

Go · PostgreSQL · Kafka · Redis · Docker · Kubernetes · Prometheus/Grafana ·
React/TypeScript · GitHub Actions. Technologies are introduced phase by phase.

## Quick start

Requires Go 1.24+ and Docker.

```bash
make up           # start Postgres
make migrate-up   # create the schema
make test         # run unit tests
make run-api      # start the API on :8080
curl localhost:8080/healthz
```

Run `make help` for all commands.

Run the Postgres integration tests too (they use a throwaway schema, so your
data is safe):

```bash
export PULSE_TEST_DATABASE_URL='postgres://pulse:pulse@localhost:5432/pulse?sslmode=disable'
make test
```

## API

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/monitors` | Create a monitor (`201`) |
| `GET` | `/api/v1/monitors` | List monitors, newest first |
| `GET` | `/api/v1/monitors/{id}` | Get one monitor |
| `PATCH` | `/api/v1/monitors/{id}` | Change any fields; `{"enabled": false}` pauses |
| `DELETE` | `/api/v1/monitors/{id}` | Delete a monitor and its results (`204`) |
| `GET` | `/healthz` | Liveness: the process is up |
| `GET` | `/readyz` | Readiness: the database answers |

```bash
curl -X POST localhost:8080/api/v1/monitors \
  -d '{"name": "Example", "url": "https://example.com", "interval_seconds": 30}'
curl localhost:8080/api/v1/monitors
curl -X PATCH localhost:8080/api/v1/monitors/<id> -d '{"enabled": false}'
```

Only `name` and `url` are required. Defaults: `GET`, every 60s, 10s timeout,
expecting status 200. Errors always look like
`{"error": {"code": "...", "message": "...", "details": [...]}}`.

## Repository layout

| Path | Purpose |
|---|---|
| `backend/cmd/` | One entry point per service (`api` now; `scheduler`, `worker` later) |
| `backend/internal/` | Application code: `config`, `domain`, `api` (+ `middleware`), `store/postgres` |
| `backend/migrations/` | SQL migrations ([golang-migrate](https://github.com/golang-migrate/migrate)) |
| `docs/adr/` | Architecture Decision Records |
| `.github/workflows/` | CI |

## Roadmap

- [x] **Phase 0** — foundations: layout, schema, CI
- [ ] Phase 1 — single-process MVP (REST API, goroutine workers, React dashboard)
- [ ] Phase 2 — split into API / scheduler / worker with Kafka
- [ ] Phase 3 — reliability: retries, idempotency, incidents, SSRF protection
- [ ] Phase 4 — Redis caching, uptime rollups, richer dashboard
- [ ] Phase 5 — Prometheus and Grafana
- [ ] Phase 6 — Kubernetes and autoscaling
- [ ] Phase 7 — full CI/CD
