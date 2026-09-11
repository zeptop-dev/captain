BIN := bin/captain
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet tidy

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/captain

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy
