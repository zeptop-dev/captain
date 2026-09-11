BIN := bin/captain
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build web test vet tidy dev-web

# Frontend first so the embedded dist is current.
build: web
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/captain

web:
	cd web/admin && pnpm install --frozen-lockfile && pnpm build

dev-web:
	cd web/admin && pnpm dev

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy
