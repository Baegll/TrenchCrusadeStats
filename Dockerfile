FROM golang:1.22-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ git && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-X main.Version=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" -o analytics ./cmd/analytics

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/analytics /usr/local/bin/analytics
WORKDIR /
EXPOSE 8080
ENTRYPOINT ["analytics"]
CMD ["serve"]
