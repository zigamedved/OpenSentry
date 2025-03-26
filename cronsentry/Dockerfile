FROM golang:1.22-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o cronsentry ./cmd

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/cronsentry .
COPY --from=builder /app/internal/db/schema.sql ./internal/db/schema.sql
COPY --from=builder /app/internal/templates ./internal/templates

ENV PORT=8080

EXPOSE 8080
CMD ["./cronsentry"] 