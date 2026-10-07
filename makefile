.DEFAULT_GOAL := help
.PHONY: help up down reset logs psql migrate-up migrate-down migrate-new run-api fmt lint test tidy

COMPOSE ?= docker compose
POSTGRES_USER ?= pulse
POSTGRES_DB   ?= pulse

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## Start local infrastructure (Postgres)
	$(COMPOSE) up -d --wait postgres

down: ## Stop local infrastructure (keeps data)
	$(COMPOSE) down

reset: ## Stop everything and DELETE all local data
	$(COMPOSE) down -v

logs: ## Tail infrastructure logs
	$(COMPOSE) logs -f

psql: ## Open a psql shell in the Postgres container
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

migrate-up: ## Apply all pending migrations
	$(COMPOSE) run --rm migrate up

migrate-down: ## Roll back the most recent migration
	$(COMPOSE) run --rm migrate down 1

migrate-new: ## Create a migration pair: make migrate-new name=add_incidents
	@test -n "$(name)" || (echo "usage: make migrate-new name=<snake_case_name>" && exit 1)
	$(COMPOSE) run --rm migrate create -ext sql -dir /migrations -seq $(name)

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

fmt: ## Format Go code
	cd backend && gofmt -s -w .

lint: ## Run golangci-lint (must be installed locally)
	cd backend && golangci-lint run ./...

test: ## Run Go tests with the race detector
	cd backend && go test -race -count=1 ./...

tidy: ## Tidy Go modules
	cd backend && go mod tidy