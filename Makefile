.PHONY: build test lint fmt run clean vet ci sync-github

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

## GitHub Sync

GITHUB_AUTHOR_NAME := mio
GITHUB_AUTHOR_EMAIL := freegalaxy@foxmail.com
GITHUB_REMOTE := github
GITHUB_BRANCH := github-main
MAIN_BRANCH := main

sync-github: ## Squash-merge main into github-main and push to GitHub
	@echo "=== Syncing $(MAIN_BRANCH) → $(GITHUB_BRANCH) ==="
	@CURRENT=$$(git rev-parse --abbrev-ref HEAD); \
	git checkout $(GITHUB_BRANCH) && \
	git merge $(MAIN_BRANCH) --squash --allow-unrelated-histories && \
	GIT_COMMITTER_NAME="$(GITHUB_AUTHOR_NAME)" \
	GIT_COMMITTER_EMAIL="$(GITHUB_AUTHOR_EMAIL)" \
	git commit --author="$(GITHUB_AUTHOR_NAME) <$(GITHUB_AUTHOR_EMAIL)>" \
		-m "sync: squash merge from $(MAIN_BRANCH) $$(git log $(MAIN_BRANCH) -1 --format='(%h)')" && \
	git push $(GITHUB_REMOTE) $(GITHUB_BRANCH) && \
	git checkout $$CURRENT && \
	echo "=== Done! Pushed to $(GITHUB_REMOTE)/$(GITHUB_BRANCH) ==="

## Cleanup

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) coverage.out dist/

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
