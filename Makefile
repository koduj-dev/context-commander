.PHONY: dev-up dev-down build ui test lint migrate migrate-down tidy

GOBIN := $(shell go env GOPATH)/bin
DB_OWNER_URL ?= postgres://cc_owner:cc_owner_dev_password@localhost:5432/context_commander?sslmode=disable

dev-up:
	docker compose -f deploy/docker-compose.yml up -d
	@echo "Postgres on localhost:5432 — add --profile s3 or --profile ldap for those"

dev-down:
	docker compose -f deploy/docker-compose.yml down

## ui: build the web frontend into web/dist (embedded into ccd via go:embed)
#
# `npm ci`, not `npm install`: web/dist is committed (see web/.gitignore) so
# `go build` never depends on Node — but that only holds if this target
# reproduces the exact dependency tree the committed dist was built from.
ui:
	cd web && npm ci && npm run build

## build: build the web UI, then the ccd binary with it embedded
build: ui
	go build -o bin/ccd ./cmd/ccd

# Integration tests spin up a real Postgres via testcontainers-go
# (internal/dbtest) — Docker must be running. Real DB, not mocks: see
# docs/architecture-cookbook.md "Testování".
test:
	go test ./... -race -count=1

lint:
	@command -v golangci-lint >/dev/null || (echo "install: https://golangci-lint.run/welcome/install/" && exit 1)
	golangci-lint run ./...

tidy:
	go mod tidy

# Migrations run as cc_owner (the table-owning role), never as cc_app.
# `./bin/ccd migrate` (embedded migrations, no external tool) does the same
# thing and is what a real deployment runs — this target is a convenience
# for iterating without rebuilding.
migrate:
	@command -v migrate >/dev/null || (echo "install: https://github.com/golang-migrate/migrate" && exit 1)
	migrate -path migrations -database "$(DB_OWNER_URL)" up

migrate-down:
	@command -v migrate >/dev/null || (echo "install: https://github.com/golang-migrate/migrate" && exit 1)
	migrate -path migrations -database "$(DB_OWNER_URL)" down 1
