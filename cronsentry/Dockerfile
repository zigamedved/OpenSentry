FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o cronsentry ./cmd

# Use a minimal alpine image for the final container
FROM alpine:latest

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/cronsentry .

# Copy necessary files
COPY --from=builder /app/internal/db/schema.sql ./internal/db/schema.sql
COPY --from=builder /app/internal/templates ./internal/templates
COPY --from=builder /app/static ./static

# Create non-root user
RUN adduser -D -g '' cronsentry && \
  chown -R cronsentry:cronsentry /app

USER cronsentry

# Set environment variables
ENV PORT=8080

# Expose the port
EXPOSE 8080

# Command to run
CMD ["./cronsentry"] 