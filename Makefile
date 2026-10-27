.PHONY: dev-up dev-down build ui test lint migrate migrate-down tidy release man clean

BINARY  := ccd
PKG     := ./cmd/ccd
OUT     := dist-bin
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
MANDIR  := dist-man
# Pages are dated by the commit, not the build, so rebuilding a tag
# reproduces them byte for byte.
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null)
export SOURCE_DATE_EPOCH

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
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

## release: cross-compile static binaries for every supported platform, plus
## SHA256SUMS — what release.yml publishes. ccd is a server on Linux, but the
## CLI half (sync-skills, token, context) runs on developers' Macs and Windows
## machines too, so all three get binaries.
release: ui
	@mkdir -p $(OUT)
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-linux-amd64       $(PKG)
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-linux-arm64       $(PKG)
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-darwin-amd64      $(PKG)
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-darwin-arm64      $(PKG)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-windows-amd64.exe $(PKG)
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/$(BINARY)-windows-arm64.exe $(PKG)
	@cd $(OUT) && sha256sum $(BINARY)-* > SHA256SUMS
	@echo "binaries + SHA256SUMS written to $(OUT)/ (version $(VERSION))"
	$(MAKE) man

## man: generate the manual pages from the command tree into dist-man/,
## gzipped as Debian policy wants. The .deb and .rpm ship them. They are not
## committed: generated at build time, they cannot drift from --help. Kept
## out of dist-bin/ because the release uploads everything in that directory.
man:
	rm -rf $(MANDIR) && mkdir -p $(MANDIR)
	go run -ldflags "$(LDFLAGS)" $(PKG) man $(MANDIR)
	gzip -9n $(MANDIR)/*.1

clean:
	rm -rf bin $(OUT) $(MANDIR) out

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
