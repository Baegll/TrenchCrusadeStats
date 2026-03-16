FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o analytics ./cmd/analytics

FROM alpine:3.19
RUN apk add --no-cache libc6-compat
COPY --from=builder /app/analytics /usr/local/bin/analytics
WORKDIR /
EXPOSE 8080
ENTRYPOINT ["analytics"]
CMD ["serve"]
