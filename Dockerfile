# =========================
# Build stage
# =========================
FROM golang:1.26 AS builder

WORKDIR /app

# Copy dependency files dulu
COPY go.mod go.sum ./

RUN go mod download

# Copy source code
COPY . .

# Build aplikasi
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o app .

# =========================
# Runtime stage
# =========================
FROM debian:bookworm-slim

WORKDIR /app

# Install CA certificates
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/app .

EXPOSE 8080

CMD ["./app"]