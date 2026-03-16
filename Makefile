.PHONY: all build test lint dev docker clean

all: lint test build

build:
	go build -o bin/analytics ./cmd/analytics

test:
	go test ./...

lint:
	golangci-lint run ./...

dev:
	go run ./cmd/analytics serve

docker:
	docker build -t trench-analytics .

clean:
	rm -rf bin/
