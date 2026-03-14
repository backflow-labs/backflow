# Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /build

# Download dependencies first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Build the binary (CGO required for go-sqlite3)
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -o backflow ./cmd/backflow

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Non-root user
RUN useradd -r -u 1000 -m backflow

# Persistent storage for SQLite database
RUN mkdir -p /data && chown backflow:backflow /data

WORKDIR /app
COPY --from=builder /build/backflow .

USER backflow

EXPOSE 8080

ENV BACKFLOW_LISTEN_ADDR=:8080
ENV BACKFLOW_DB_PATH=/data/backflow.db

ENTRYPOINT ["/app/backflow"]
