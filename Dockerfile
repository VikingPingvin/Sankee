# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /sankee .

# ── Runtime stage ─────────────────────────────────────────────────────────────
# alpine gives us ca-certificates (needed if connecting to Postgres over TLS).
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /sankee /sankee

EXPOSE 8080

# Override with SANKEE_DB_DSN for a custom path or Postgres connection string.
ENV SANKEE_ADDR=0.0.0.0:8080 \
    SANKEE_DB_DRIVER=sqlite \
    SANKEE_DB_DSN=/data/sankee.db

ENTRYPOINT ["/sankee"]
