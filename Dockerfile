# Stage 1: Build the Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency files first so Docker can cache the download layer
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code and build
COPY . .
RUN go build -o bot ./cmd/bot

# Stage 2: Run the binary in a minimal image
FROM alpine:3.20

WORKDIR /app
COPY --from=builder /app/bot .

CMD ["./bot"]
