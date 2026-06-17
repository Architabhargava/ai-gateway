# ── AI Gateway Makefile ───────────────────────────────────────────────────
# Usage:
#   make run       — start the gateway locally
#   make build     — compile the binary
#   make test      — run all tests with race detector
#   make lint      — run golangci-lint
#   make docker    — build Docker image
#   make clean     — remove build artifacts

APP      := ai-gateway
BIN      := ./bin/$(APP)
PKG      := ./...
DOCKER_TAG := $(APP):latest

.PHONY: run build test lint docker clean tidy fmt

## Start the gateway locally
run:
	go run cmd/main.go

## Compile a production binary to ./bin/ai-gateway
build:
	mkdir -p bin
	CGO_ENABLED=0 go build \
		-ldflags="-s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" \
		-o $(BIN) \
		./cmd/main.go
	@echo "Binary: $(BIN) ($(shell du -sh $(BIN) | cut -f1))"

## Run all tests with race detector and coverage
test:
	go test -v -race -count=1 -timeout=60s $(PKG)

## Run tests and output coverage report
cover:
	go test -race -coverprofile=coverage.out -covermode=atomic $(PKG)
	go tool cover -func=coverage.out
	@echo "Open coverage.html: go tool cover -html=coverage.out"

## Run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

## Format all Go files
fmt:
	gofmt -w .
	go vet ./...

## Download and tidy dependencies
tidy:
	go mod tidy

## Build Docker image
docker:
	docker build -t $(DOCKER_TAG) .
	@echo "Image: $(DOCKER_TAG)"
	@docker image inspect $(DOCKER_TAG) --format='Size: {{.Size}}' 2>/dev/null | awk '{printf "Image size: %.1fMB\n", $$2/1024/1024}'

## Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html
	go clean -cache

## Show help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'