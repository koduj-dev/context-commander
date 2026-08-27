.PHONY: dev-up dev-down build test lint migrate migrate-down tidy

GOBIN := $(shell go env GOPATH)/bin
DB_OWNER_URL ?= postgres://cc_owner:cc_owner_dev_password@localhost:5432/context_commander?sslmode=disable

dev-up:
	docker compose -f deploy/docker-compose.yml up -d
	@echo "Postgres on localhost:5432, MinIO on localhost:9000 (console :9001)"

dev-down:
	docker compose -f deploy/docker-compose.yml down

build:
	go build -o bin/ccd ./cmd/ccd

test:
	go test ./...

lint:
	@command -v golangci-lint >/dev/null || (echo "install: https://golangci-lint.run/welcome/install/" && exit 1)
	golangci-lint run ./...

tidy:
	go mod tidy

# Migrations run as cc_owner (the table-owning role), never as cc_app.
migrate:
	@command -v migrate >/dev/null || (echo "install: https://github.com/golang-migrate/migrate" && exit 1)
	migrate -path migrations -database "$(DB_OWNER_URL)" up

migrate-down:
	@command -v migrate >/dev/null || (echo "install: https://github.com/golang-migrate/migrate" && exit 1)
	migrate -path migrations -database "$(DB_OWNER_URL)" down 1
