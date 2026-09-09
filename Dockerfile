# =====================================
# Builder Stage
# =====================================
FROM golang:1.27-alpine AS builder

WORKDIR /app

ARG ENVIRONMENT=production

# Important: CGO enabled for go-sqlite3
ENV CGO_ENABLED=1

# Install build deps for CGO + SQLite (aligned with go-sqlite3 Alpine guidance)
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Fix musl sqlite3 LFS symbols (pread64/pwrite64/off64_t)
ENV CGO_CFLAGS="-Dpread64=pread -Dpwrite64=pwrite -Doff64_t=off_t"

# Install migrate binary (SQLite driver requires CGO + sqlite3 build tag)
ARG MIGRATE_VERSION=v4.17.1
RUN GOBIN=/app/bin go install -tags 'sqlite3' github.com/golang-migrate/migrate/v4/cmd/migrate@${MIGRATE_VERSION}

# Download dependencies first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build asset cache-busting versions
RUN ENVIRONMENT=$ENVIRONMENT go run ./cmd/assets

# Build Go binary with CGO enabled (needed for SQLite)
RUN GOOS=linux go build -o /app/nw-kids-checkout .

# =====================================
# Final Stage
# =====================================
FROM alpine:3.24

# Install runtime dependencies: CA certs for TLS, SQLite libs/CLI
RUN apk add --no-cache ca-certificates sqlite-libs sqlite tzdata

WORKDIR /app

# Copy the compiled Go binary
COPY --from=builder /app/nw-kids-checkout .

# Copy migrate binary and migration files
COPY --from=builder /app/bin/migrate /usr/local/bin/migrate
COPY db/migrations /app/db/migrations

# Create a directory for SQLite database + WAL/SHM
RUN mkdir /data

# Mark /data as a volume (Podman can mount persistent storage here)
VOLUME /data

# Default environment variables
ARG RUN_MODE=apiserver
ENV DB_FILE=/data/kids-checkin.db \
    PORT=3000 \
    RUN_MODE=$RUN_MODE

# Create a non-root user for prod (UID 1001)
RUN addgroup -g 1001 app && adduser -D -u 1001 -G app app

# Set default user to non-root (can be overridden in dev)
USER app

# Expose port
EXPOSE $PORT

# Default runs apiserver. Build with --build-arg RUN_MODE=checkout-fetcher
# (or set the RUN_MODE env var at runtime) to run the fetcher as a service.
CMD ["sh", "-c", "if [ \"$RUN_MODE\" = \"checkout-fetcher\" ]; then exec ./nw-kids-checkout checkout-fetcher --service --use-check-windows; else exec ./nw-kids-checkout apiserver; fi"]
