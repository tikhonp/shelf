# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27.3

# web: the Vue build, which shelf embeds. Its output is the same for every
# platform, so it runs on the build machine's.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM golang:${GO_VERSION}-alpine AS base

# dev: the compose stack's hot-reload container. goose matches the version
# in go.mod.
FROM base AS dev
RUN go install github.com/air-verse/air@v1.67.4 && \
    go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
CMD ["air", "-c", ".air.toml"]

# lint: `make lint` builds this stage. The caches are BuildKit cache mounts,
# so the stage fetches modules once and analyzes only changed packages again.
# Keep the versions equal to CI's.
FROM golangci/golangci-lint:v2.15.0 AS lint
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache \
    --mount=type=bind,target=. \
    golangci-lint run && \
    go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

# build: one static binary from a bind-mounted source that never lands in an
# image, with the web stage's build mounted over the committed placeholder.
# /out/data becomes /data, owned by the runtime user, so a fresh named
# volume is writable.
FROM --platform=$BUILDPLATFORM base AS build
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,target=. \
    --mount=type=bind,from=web,source=/src/internal/platform/ui/dist,target=/src/internal/platform/ui/dist \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/tikhonp/shelf/internal/platform/obs.AppVersion=${APP_VERSION}" \
    -o /out/shelf ./cmd/shelf && \
    mkdir /out/data

# build-wrapper: the supervisor of the shelf-wrapper image, static and with
# no shelf packages but internal/wrapperapi.
FROM --platform=$BUILDPLATFORM base AS build-wrapper
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=${APP_VERSION}" \
    -o /out/shelf-wrapper ./cmd/shelf-wrapper

# wrapper: wrapper-lite with the supervisor as its entrypoint, in place of
# the image's entrypoint.sh, which checks an outdated account database path.
# Build it with --platform linux/amd64: wrapper-lite is amd64 only. The base
# is pinned by digest and bumped on purpose; look the digest up with
# `docker buildx imagetools inspect ghcr.io/tikhonp/wrapper-lite:latest`.
# tini is PID 1, so it reaps the wrapper-lite processes a SIGKILL orphans,
# and passes Docker's SIGTERM to the supervisor.
FROM ghcr.io/tikhonp/wrapper-lite@sha256:d6a49555e7640c79e33b497ac8f5de86f1e0b9d5f3f856a480fea0f928f87728 AS wrapper
RUN apt-get update && apt-get install -y --no-install-recommends tini && rm -rf /var/lib/apt/lists/*
COPY --from=build-wrapper /out/shelf-wrapper /usr/local/bin/shelf-wrapper
ARG APP_VERSION=dev
ARG BUILD_DATE=
ARG VCS_REF=
LABEL org.opencontainers.image.title="Music Shelf wrapper" \
      org.opencontainers.image.description="wrapper-lite for Music Shelf, with a supervisor that signs it in" \
      org.opencontainers.image.source="https://code.tikhonnnnn.com/tikhon/shelf" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
# Dozzle shows this icon for the container: shelf's record with a rose label,
# so the two look related in Dozzle but are easy to tell apart.
LABEL dev.dozzle.icon="data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iNjAiIGhlaWdodD0iNjAiIHZpZXdCb3g9IjAgMCA2MCA2MCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iNjAiIGhlaWdodD0iNjAiIHJ4PSI4IiBmaWxsPSIjM2Q0YTU3Ii8+PGNpcmNsZSBjeD0iMzAiIGN5PSIzMCIgcj0iMjEiIGZpbGw9IiMxNTE3MWEiLz48Y2lyY2xlIGN4PSIzMCIgY3k9IjMwIiByPSIxNSIgZmlsbD0ibm9uZSIgc3Ryb2tlPSIjMmEyZTMzIiBzdHJva2Utd2lkdGg9IjEuNSIvPjxjaXJjbGUgY3g9IjMwIiBjeT0iMzAiIHI9IjciIGZpbGw9IiNlNTU2NmIiLz48Y2lyY2xlIGN4PSIzMCIgY3k9IjMwIiByPSIxLjYiIGZpbGw9IiMxNTE3MWEiLz48L3N2Zz4="
WORKDIR /app
EXPOSE 12340 12341
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/shelf-wrapper"]
# The base image's CMD would reach the supervisor as an argument.
CMD []

# prod: the runtime image, and the last stage, so `docker build .` builds it.
# amdl and the Qobuz segmented fallback call ffmpeg. amdl is the upstream
# static binary, the one that runs in production today.
FROM alpine:3.22 AS prod
RUN apk add --no-cache ca-certificates ffmpeg tzdata
COPY --from=ghcr.io/zhaarey/apple-music-downloader:0.1.4 /usr/local/bin/amdl /usr/local/bin/amdl
COPY --from=build /out/shelf /usr/local/bin/shelf
COPY --from=build --chown=10001:10001 /out/data /data

ARG APP_VERSION=dev
ARG BUILD_DATE=
ARG VCS_REF=
LABEL org.opencontainers.image.title="Music Shelf" \
      org.opencontainers.image.description="Downloads from Qobuz and Apple Music into a Navidrome library" \
      org.opencontainers.image.source="https://code.tikhonnnnn.com/tikhon/shelf" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
# Dozzle shows this icon for the container: a record with an amber label on
# a blue-grey panel, as a base64 SVG.
LABEL dev.dozzle.icon="data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iNjAiIGhlaWdodD0iNjAiIHZpZXdCb3g9IjAgMCA2MCA2MCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iNjAiIGhlaWdodD0iNjAiIHJ4PSI4IiBmaWxsPSIjM2Q0YTU3Ii8+PGNpcmNsZSBjeD0iMzAiIGN5PSIzMCIgcj0iMjEiIGZpbGw9IiMxNTE3MWEiLz48Y2lyY2xlIGN4PSIzMCIgY3k9IjMwIiByPSIxNSIgZmlsbD0ibm9uZSIgc3Ryb2tlPSIjMmEyZTMzIiBzdHJva2Utd2lkdGg9IjEuNSIvPjxjaXJjbGUgY3g9IjMwIiBjeT0iMzAiIHI9IjciIGZpbGw9IiNlOGEzM2QiLz48Y2lyY2xlIGN4PSIzMCIgY3k9IjMwIiByPSIxLjYiIGZpbGw9IiMxNTE3MWEiLz48L3N2Zz4="

# UID 10001 owns the music folder; see docs/deployment.md.
USER 10001:10001
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/shelf"]
CMD ["serve"]
