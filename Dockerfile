# syntax=docker/dockerfile:1

# service-api-standard: metacensus/api's service over Postgres.

# --- Build stage: cross-compile the service ----------------------------------
# Pinned to the native build platform, not the target: Go cross-compiles, so the
# compile runs once at native speed and emits a binary per target instead of
# running the compiler under QEMU. buildx supplies TARGETOS/TARGETARCH.
# go.mod's `go` directive is a floor; the image tag floats within 1.27.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

# Dependencies first, against the committed go.mod/go.sum, so a source-only
# change reuses this layer. The build never runs `go mod tidy`: a dependency
# changes in a reviewed commit, not at build time.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static binary.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/service

# --- Runner stage ------------------------------------------------------------
# All configuration comes from the environment (DATABASE_URL, PORT); nothing is
# baked in, so one image serves every deployment.
FROM alpine:3.23 AS runner
WORKDIR /app
ENV PORT=3001

# Alpine ships no trust store, and a managed Postgres is reached over verified
# TLS.
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/service /app/service

# Nothing here needs root, and the service never writes to the filesystem.
RUN addgroup -S app && adduser -S -G app app
USER app

EXPOSE ${PORT}

# BusyBox wget is in the base.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/healthz" || exit 1

ENTRYPOINT ["/app/service"]
