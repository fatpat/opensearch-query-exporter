# Build stage
FROM golang:1.21-alpine AS builder

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o opensearch-query-exporter \
    cmd/exporter/main.go

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 -S exporter && \
    adduser -u 1000 -S exporter -G exporter

# Copy binary from build stage
COPY --from=builder /build/opensearch-query-exporter /usr/local/bin/opensearch-query-exporter

# Copy example configs
COPY --from=builder /build/configs /etc/opensearch-query-exporter/

# Create directory for custom configs
RUN mkdir -p /config && \
    chown -R exporter:exporter /config /etc/opensearch-query-exporter

# Switch to non-root user
USER exporter

# Expose metrics port
EXPOSE 9206

# Set default config path
ENV CONFIG_PATH=/config/config.yaml

# Run the exporter
ENTRYPOINT ["opensearch-query-exporter"]
CMD ["-config", "/config/config.yaml"]
