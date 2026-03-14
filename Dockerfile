# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.24-bookworm AS builder

WORKDIR /build

# Download dependencies first for layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is required by go-sqlite3
RUN CGO_ENABLED=1 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o backflow \
    ./cmd/backflow

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Non-root user
RUN useradd -r -u 1001 -g root backflow

WORKDIR /app

COPY --from=builder /build/backflow .

# /data holds the SQLite database; mount a volume here for persistence
RUN mkdir -p /data && chown backflow:root /data

USER backflow

EXPOSE 8080

ENV BACKFLOW_LISTEN_ADDR=:8080 \
    BACKFLOW_DB_PATH=/data/backflow.db

VOLUME ["/data"]

ENTRYPOINT ["/app/backflow"]
