FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/platform ./cmd/platform

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/platform /app/platform
COPY manifest/config/config.yaml /app/config/config.yaml

EXPOSE 8090
ENTRYPOINT ["/app/platform"]
