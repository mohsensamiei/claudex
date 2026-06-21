# Build stage - compile the Go binary
FROM --platform=linux/arm64 golang:1.24-alpine AS builder

WORKDIR /src

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -ldflags="-s -w" -o /out/server ./cmd/server

# Runtime stage
FROM --platform=linux/arm64 node:22-alpine

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Install Claude CLI globally
RUN npm install -g @anthropic-ai/claude-code

# Create non-root user with home directory
RUN adduser -D -g '' -h /home/appuser appuser

# Create .claude directory for credentials with write permissions
RUN mkdir -p /home/appuser/.claude && \
    chown -R appuser:appuser /home/appuser && \
    chmod 755 /home/appuser/.claude

# Copy built binary from the build stage and entrypoint
COPY --from=builder /out/server /app/server
COPY scripts/entrypoint.sh /app/entrypoint.sh

# Change ownership and permissions
RUN chown -R appuser:appuser /app && \
    chmod +x /app/server /app/entrypoint.sh

# Set HOME for Claude CLI
ENV HOME=/home/appuser

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

# Run via entrypoint script for credential handling
ENTRYPOINT ["/app/entrypoint.sh"]
