# syntax=docker/dockerfile:1

# 1. Build the static binary. The committed web/dist is embedded via go:embed,
#    so the image is fully self-contained and no Node stage is required.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/ccd ./cmd/ccd

# 2. Minimal runtime: distroless static ships CA certificates (TLS to
#    Postgres/S3) and has no shell or package manager.
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/koduj-dev/context-commander" \
      org.opencontainers.image.description="Self-hosted, vendor-neutral shared context and collaboration layer for humans and AI agents." \
      org.opencontainers.image.licenses="AGPL-3.0"
COPY --from=build /out/ccd /usr/local/bin/ccd
# Artifact storage (filesystem backend) lives on this volume, not in the
# image — see deploy/docker-compose.yml.
ENV CC_STORAGE_DIR=/data
EXPOSE 8490
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/ccd"]
# `serve` binds 0.0.0.0 inside the container by design (Docker's own network
# isolation is the boundary here, matching postgres/minio in
# deploy/docker-compose.yml, which are only published to 127.0.0.1 on the
# host) — --insecure-allow-cleartext overrides ccd's own refusal to bind a
# non-loopback address without TLS, which is otherwise correct for a bare
# `ccd serve` on an actual network interface.
CMD ["serve", "--addr", "0.0.0.0:8490", "--insecure-allow-cleartext"]
