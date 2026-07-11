# --- build stage ---
# Alpine + build-base gives us the C toolchain that mattn/go-sqlite3 (cgo) needs.
FROM golang:1.22-alpine AS build

RUN apk add --no-cache build-base

WORKDIR /src

# Cache dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Build.
COPY . .
ENV CGO_ENABLED=1
RUN go build -ldflags="-s -w" -o /out/greenlight ./cmd/greenlight

# --- runtime stage ---
FROM alpine:3.20

# ca-certificates for outbound HTTPS (ntfy / resume URLs); tzdata for local time
# formatting in the UI.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 greenlight

WORKDIR /app
COPY --from=build /out/greenlight /app/greenlight

# Persist the SQLite database on a volume.
ENV GREENLIGHT_DB_PATH=/data/greenlight.db
RUN mkdir -p /data && chown greenlight:greenlight /data
VOLUME /data

USER greenlight
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/greenlight"]
