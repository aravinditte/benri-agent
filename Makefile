# Benri Agent — task runner (SPEC §13.1). Recipes are REAL: Docker/PG
# recipes require Docker Compose / PostgreSQL and will fail honestly where
# those are unavailable; nothing here fake-succeeds.
GO ?= go
BIN_DIR ?= bin

.PHONY: dev up down migrate seed generate generate-check test test-race lint eval eval-report build smoke

build:
	$(GO) build -o $(BIN_DIR)/ ./cmd/...

## dev: run api + web locally against the SQLite store (no Docker needed)
dev: build
	@echo "Start the API:  BENRI_DB_PATH=./data/benri.sqlite ./bin/api"
	@echo "Start the web:  cd web && npm run dev"
	@echo "Workers:        ./bin/agent-worker --once   ./bin/executor-worker --once"

## up/down: Docker Compose stack (requires Docker; NOT available on every machine)
up:
	docker compose up --build -d
down:
	docker compose down

## migrate: apply goose migrations to BENRI_MIGRATE_DATABASE_URL (PostgreSQL)
migrate:
	goose -dir db/migrations postgres "$(BENRI_MIGRATE_DATABASE_URL)" up

## seed: seed the configured database from synthetic fixtures (idempotent)
seed: build
	BENRI_DB_PATH=$${BENRI_DB_PATH:-./data/benri.sqlite} ./bin/seed

## generate: regenerate OpenAPI (Go + TS) and sqlc outputs from canonical sources
generate:
	oapi-codegen -generate types,chi-server -package gen -o internal/api/gen/api.gen.go api/openapi.yaml
	sqlc generate -f db/sqlc.yaml
	cd web && npm run generate:api

## generate-check: drift check — regenerate into temp and diff (non-zero on drift)
generate-check:
	@tmp=$$(mktemp -d); \
	oapi-codegen -generate types,chi-server -package gen -o $$tmp/api.gen.go api/openapi.yaml; \
	diff -u internal/api/gen/api.gen.go $$tmp/api.gen.go || { echo "DRIFT: internal/api/gen/api.gen.go"; exit 1; }; \
	cp web/lib/api/schema.d.ts $$tmp/schema.d.ts; \
	cd web && npx openapi-typescript ../api/openapi.yaml -o $$tmp/schema.new.d.ts; cd ..; \
	diff -u $$tmp/schema.d.ts $$tmp/schema.new.d.ts || { echo "DRIFT: web/lib/api/schema.d.ts"; exit 1; }; \
	cp -r internal/store/dbgen $$tmp/dbgen-orig; \
	sqlc generate -f db/sqlc.yaml; \
	diff -r $$tmp/dbgen-orig internal/store/dbgen || { echo "DRIFT: internal/store/dbgen"; exit 1; }; \
	echo "generate-check: no drift"

test:
	$(GO) test ./...
	cd web && npm run typecheck && npm test

test-race:
	$(GO) test -race ./...

lint:
	@test -z "$$($(GO) fmt -l . 2>/dev/null || gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "gofmt drift"; exit 1; }
	$(GO) vet ./...
	cd web && npm run lint

eval: build
	BENRI_ROOT_DIR=. BENRI_DB_PATH=$${BENRI_DB_PATH:-./data/benri.sqlite} ./bin/eval

eval-report:
	@cat evals/reports/latest.md

smoke: build
	BENRI_ROOT_DIR=. ./bin/smoke
