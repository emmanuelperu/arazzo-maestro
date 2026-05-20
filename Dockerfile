# syntax=docker/dockerfile:1
#
# Multi-stage build producing a `FROM scratch` final image.
# A static `arazzo-maestro` binary, nothing else. No shell, no libc,
# no package manager — the smallest possible attack surface.
#
# Build with:
#   docker build --build-arg VERSION=0.0.1 \
#     -t ghcr.io/emmanuelperu/arazzo-maestro:0.0.1 .
#
# In OpenSSF Phase 2 (see Plan.md) `goreleaser` will produce the
# release images and sign them with cosign; this Dockerfile is the
# local-dev / opt-out path.

ARG GO_VERSION=1.23

# ---- builder ------------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

# Pull modules first so the layer caches between source-only edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=0.0.1
ENV CGO_ENABLED=0 \
    GOFLAGS=-mod=readonly

RUN go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/arazzo-maestro \
        ./cmd/arazzo-maestro

# ---- final image --------------------------------------------------------
FROM scratch

# OCI labels make the image self-describing on registries.
LABEL org.opencontainers.image.title="arazzo-maestro" \
      org.opencontainers.image.description="Lint and render Arazzo workflow specifications." \
      org.opencontainers.image.source="https://github.com/emmanuelperu/arazzo-maestro" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=builder /out/arazzo-maestro /arazzo-maestro

ENTRYPOINT ["/arazzo-maestro"]
