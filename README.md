# Pulse

A distributed API monitoring platform. Add an endpoint and Pulse checks it on a
schedule, tracking uptime, status codes, latency, failures and incidents.

> **Status: Phase 1 in progress.** Monitor CRUD API, the scheduler and the
> goroutine worker pool are done: monitors are checked automatically and every
> result is stored. Next: the React dashboard.

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

## Try it: watch Pulse monitor something

Open three terminals (from the repo root):

```bash
# 1. the API (also runs the scheduler and workers)
make run-api

# 2. a fake target you can break on purpose
make run-mock

# 3. create a monitor and look at its results
curl -X POST localhost:8080/api/v1/monitors \
  -d '{"name": "switch", "url": "http://localhost:9000/switch", "interval_seconds": 10}'
curl localhost:8080/api/v1/monitors/MONITOR_ID/results

curl -X POST localhost:9000/switch/down   # start an outage: checks now fail
curl -X POST localhost:9000/switch/up     # recover
```

The mock target also has `/ok`, `/slow?ms=2000`, `/status/503` and `/flaky?fail=0.3`.

Settings (all optional, see `.env.example`): `PULSE_WORKERS` (default 10, `0`
turns monitoring off), `PULSE_SCHEDULER_TICK` (default 1s).

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
| `GET` | `/api/v1/monitors/{id}/results` | Check history, newest first (`?limit=50&before=<time>`) |
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
| `backend/cmd/` | One entry point per service (`api` and `mocktarget` now; `scheduler`, `worker` later) |
| `backend/internal/` | Application code: `config`, `domain`, `api` (+ `middleware`), `store/postgres`, `scheduler`, `worker` |
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
