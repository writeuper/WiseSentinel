# WiseSentinel Platform Makefile
# Phase 1: development workflow commands

GO       ?= go
BIN      ?= platform
OUT_DIR  ?= ./build
MAIN     ?= ./cmd/platform

# ────────────────────────────────────────────────────────────────────
# Build
# ────────────────────────────────────────────────────────────────────

.PHONY: build
build: ## Compile the platform binary
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 $(GO) build -o $(OUT_DIR)/$(BIN) $(MAIN)

.PHONY: clean
clean: ## Remove build artifacts
	@rm -rf $(OUT_DIR)

# ────────────────────────────────────────────────────────────────────
# Run
# ────────────────────────────────────────────────────────────────────

.PHONY: run
run: ## Run the platform locally (requires infra services)
	$(GO) run $(MAIN)

# ────────────────────────────────────────────────────────────────────
# Test
# ────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run all unit tests
	$(GO) test ./api/... ./cmd/... ./internal/... -count=1 -short

.PHONY: test-race
test-race: ## Run unit tests with race detection
	$(GO) test ./api/... ./cmd/... ./internal/... -count=1 -short -race

.PHONY: test-integration
test-integration: ## Run integration tests (requires Milvus)
	$(GO) test ./internal/rag/... -count=1 -tags=integration -v

.PHONY: test-verbose
test-verbose: ## Run unit tests with verbose output
	$(GO) test ./api/... ./cmd/... ./internal/... -count=1 -short -v

.PHONY: test-coverage
test-coverage: ## Run unit tests and generate coverage report
	@mkdir -p $(OUT_DIR)
	$(GO) test ./api/... ./cmd/... ./internal/... -count=1 -short -coverprofile=$(OUT_DIR)/coverage.out
	$(GO) tool cover -html=$(OUT_DIR)/coverage.out -o $(OUT_DIR)/coverage.html
	@echo "Coverage report: $(OUT_DIR)/coverage.html"

# ────────────────────────────────────────────────────────────────────
# Lint
# ────────────────────────────────────────────────────────────────────

.PHONY: lint
lint: ## Run golangci-lint
	@which golangci-lint >/dev/null 2>&1 || (echo "golangci-lint not installed, run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./api/... ./cmd/... ./internal/...

# ────────────────────────────────────────────────────────────────────
# Docker
# ────────────────────────────────────────────────────────────────────

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -f manifest/docker/Dockerfile -t wisesentinel-platform:latest .

.PHONY: docker-up
docker-up: ## Start all services with docker-compose
	docker compose -f manifest/docker/docker-compose.yml up -d

.PHONY: docker-down
docker-down: ## Stop all services
	docker compose -f manifest/docker/docker-compose.yml down

.PHONY: docker-logs
docker-logs: ## Tail logs from all services
	docker compose -f manifest/docker/docker-compose.yml logs -f

# ────────────────────────────────────────────────────────────────────
# Dependencies
# ────────────────────────────────────────────────────────────────────

.PHONY: deps
deps: ## Download Go module dependencies
	$(GO) mod tidy
	$(GO) mod download

.PHONY: deps-update
deps-update: ## Update all dependencies
	$(GO) get -u ./...
	$(GO) mod tidy

# ────────────────────────────────────────────────────────────────────
# Help
# ────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help