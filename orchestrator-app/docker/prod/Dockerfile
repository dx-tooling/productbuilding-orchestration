# Production multi-stage build
FROM golang:1.26 AS builder

WORKDIR /app

# Download dependencies first (cacheable layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server binary (pure Go, no CGO needed thanks to modernc.org/sqlite)
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/server ./cmd/server

# Runtime stage — docker:cli for Docker Compose access (orchestrator shells out to compose)
FROM docker:27-cli

WORKDIR /app

COPY --from=builder /app/bin/server /app/server
COPY --from=builder /app/migrations /app/migrations

EXPOSE 8080

ENTRYPOINT ["/app/server"]
