FROM golang:1.26

# Install air for hot reload
RUN go install github.com/air-verse/air@latest

WORKDIR /app

# Keep the container running (dev commands run via docker compose exec)
CMD ["tail", "-f", "/dev/null"]
