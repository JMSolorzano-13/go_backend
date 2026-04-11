ARG TARGET=dev

# ---------------------------------------------------------------------------
# Build stage
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /go-backend ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /go-worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cron ./cmd/cron

# ---------------------------------------------------------------------------
# Dev image — alpine with shell for debugging
# ---------------------------------------------------------------------------
FROM alpine:3.19 AS dev
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=builder /go-backend /usr/local/bin/go-backend
COPY --from=builder /go-worker /usr/local/bin/go-worker
EXPOSE 8001
ENTRYPOINT ["go-backend"]

# ---------------------------------------------------------------------------
# Dev worker — same binaries, Service Bus consumer entrypoint
# ---------------------------------------------------------------------------
FROM alpine:3.19 AS dev-worker
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=builder /go-backend /usr/local/bin/go-backend
COPY --from=builder /go-worker /usr/local/bin/go-worker
ENTRYPOINT ["go-worker"]

# ---------------------------------------------------------------------------
# Prod image — scratch for minimal footprint
# ---------------------------------------------------------------------------
FROM scratch AS prod
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go-backend /go-backend
EXPOSE 8001
ENTRYPOINT ["/go-backend"]

# ---------------------------------------------------------------------------
# Prod worker — SAT pipeline consumer (no HTTP port)
# ---------------------------------------------------------------------------
FROM scratch AS prod-worker
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go-worker /go-worker
ENTRYPOINT ["/go-worker"]

# ---------------------------------------------------------------------------
# Prod cron — scheduled SAT sync jobs (no HTTP port)
# ---------------------------------------------------------------------------
FROM scratch AS prod-cron
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /cron /cron
ENTRYPOINT ["/cron"]
