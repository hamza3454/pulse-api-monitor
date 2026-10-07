# Pulse

A distributed API monitoring platform. Add an endpoint and Pulse checks it on a
schedule, tracking uptime, status codes, latency, failures and incidents.

> **Status: Phase 0 (foundations).** Repo scaffolding, Postgres schema, CI. The
> monitoring pipeline itself arrives in Phases 1–2.

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

## Repository layout

| Path | Purpose |
|---|---|
| `backend/cmd/` | One entry point per service (`api` now; `scheduler`, `worker` later) |
| `backend/internal/` | Application code: `config`, `domain`, `api`, ... |
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
