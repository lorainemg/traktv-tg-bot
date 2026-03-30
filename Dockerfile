# ── Dev stage: hot reload with Air ──
FROM golang:1.26-alpine AS dev

# Install Air for hot reloading - watches .go files and rebuilds automatically.
# "go install" downloads and compiles a Go binary into $GOPATH/bin.
RUN go install github.com/air-verse/air@latest

WORKDIR /app

# Source code is mounted via docker-compose volume, not copied.
CMD ["air"]

# ── Build stage: compile the production binary ──
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o bot ./cmd/bot

# ── Production stage: minimal image with just the binary ──
FROM alpine:3.20 AS prod

WORKDIR /app
COPY --from=builder /app/bot .

CMD ["./bot"]
