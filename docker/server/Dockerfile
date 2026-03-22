FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /backflow ./cmd/backflow

FROM gcr.io/distroless/static-debian12

COPY --from=builder /backflow /backflow
COPY --from=builder /src/migrations /migrations

ENTRYPOINT ["/backflow"]
