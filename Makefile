.PHONY: build test lint fmt run clean vet ci

BINARY := korthex
BUILD_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

## Development

build: ## Build the binary
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/korthex

run: build ## Build and run
	./$(BUILD_DIR)/$(BINARY)

test: ## Run tests with race detection
	go test -v -race -coverprofile=coverage.out ./...

test-short: ## Run tests without integration tests
	go test -v -race -short ./...

lint: ## Run linters
	golangci-lint run

fmt: ## Format code
	gofmt -w .
	goimports -w .

vet: ## Run go vet
	go vet ./...

## CI

ci: lint test build ## Run full CI pipeline

## Release

release: ## Create a release (requires goreleaser)
	goreleaser release --clean

release-snapshot: ## Create a snapshot release (local only)
	goreleaser release --snapshot --clean

## Cleanup

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) coverage.out dist/

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
