# Multi-stage build for Working Time Measurement System
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install git for Go modules that might depend on it
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests and sqlite
RUN apk --no-cache add ca-certificates sqlite

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Copy static files and templates
COPY --from=builder /app/templates ./templates/
COPY --from=builder /app/static ./static/

# Create directory for database
RUN mkdir -p /data

# Environment variables
ENV DB_BACKEND=sqlite
ENV SQLITE_PATH=/data/time_tracking.db
ENV SERVER_PORT=8083
ENV FEATURE_MULTI_TENANT=true

# Expose port
EXPOSE 8083

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8083/ || exit 1

# Run the binary
CMD ["./main"]
