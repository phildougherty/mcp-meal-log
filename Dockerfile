# Dockerfile
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# Copy source
COPY . .

# Download dependencies
RUN go mod download

# Build the binary with CGO enabled for SQLite support
RUN CGO_ENABLED=1 GOOS=linux go build -a -o meal-log ./cmd/meal-log

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache sqlite ca-certificates tzdata

# Set timezone
ENV TZ=America/New_York
RUN cp /usr/share/zoneinfo/America/New_York /etc/localtime && \
    echo "America/New_York" > /etc/timezone

WORKDIR /app

# Copy the binary
COPY --from=builder /build/meal-log /app/meal-log
RUN chmod +x /app/meal-log

# Create data directory
RUN mkdir -p /data

# Expose the HTTP port
EXPOSE 8011

# Run the server
CMD ["/app/meal-log", "--transport", "http", "--host", "0.0.0.0", "--port", "8011", "--db-path", "/data/meal-log.db"]
