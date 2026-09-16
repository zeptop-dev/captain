BIN := bin/captain
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build web test vet tidy dev-web e2e

# Frontend first so the embedded dist is current.
build: web
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/captain

web:
	cd web/admin && pnpm install --frozen-lockfile && pnpm build
	cd web/portal && pnpm install --frozen-lockfile && pnpm build && cd ../site && pnpm install --frozen-lockfile && pnpm build && cd ../probe && pnpm install --frozen-lockfile && pnpm build

dev-web:
	cd web/admin && pnpm dev

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

# Live regression against a real panel + nodes; see scripts/e2e/README.md.
e2e:
	python3 scripts/e2e/live.py
