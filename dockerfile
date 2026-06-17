# ── Stage 1: Build ────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# Install git for version injection via ldflags
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Download dependencies first (cached layer — only re-runs when go.mod changes)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build
# -ldflags="-s -w" strips debug symbols — reduces binary size ~30%
# CGO_ENABLED=0 produces a fully static binary — no C runtime dependency
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /app/gateway \
    ./cmd/main.go

# ── Stage 2: Runtime ──────────────────────────────────────────────────────
FROM alpine:3.20

# Security: run as non-root user
# nobody:nobody (uid 65534) is the standard unprivileged user in Alpine
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy only the compiled binary from builder — final image has no Go toolchain
COPY --from=builder /app/gateway .

# Create data directory for SQLite (used when mounted volume is available)
RUN mkdir -p /data && chown nobody:nobody /data

# Switch to non-root user — container runs as nobody, not root
USER nobody

EXPOSE 8080

# HEALTHCHECK instruction — used by Docker, docker-compose, and Render
# Checks every 30s, fails after 3 consecutive failures
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./gateway"]