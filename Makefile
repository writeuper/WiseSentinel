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
test-integration: ## Run RAG integration tests (requires MySQL and Milvus)
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" $(GO) test ./internal/rag/... -count=1 -tags=integration -v -timeout "$${RAG_TEST_TIMEOUT:-3m}"

.PHONY: rag-eval
rag-eval: ## Run the labelled RAG ranking regression evaluation (requires MySQL and Milvus)
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" $(GO) test ./internal/rag -count=1 -tags=integration -run '^TestRAGEvaluationMetrics$$' -v -timeout "$${RAG_TEST_TIMEOUT:-3m}"

.PHONY: test-concurrency-race
test-concurrency-race: ## Race-check concurrent tenant-isolated RAG retrieval (requires MySQL and Milvus)
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" $(GO) test -race ./internal/rag -count=1 -tags=integration -run '^TestConcurrentRAGRetrievalTenantIsolation$$' -v -timeout "$${RAG_TEST_TIMEOUT:-3m}"

.PHONY: test-real-scenarios
test-real-scenarios: ## Run MySQL/Redis/Milvus scenario, concurrency, lease and approval gates
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	@test -n "$(OPS_TEST_REDIS_ADDR)" || (echo "OPS_TEST_REDIS_ADDR is required" && exit 2)
	$(MAKE) test-integration OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" RAG_TEST_TIMEOUT="$${RAG_TEST_TIMEOUT:-3m}"
	$(MAKE) test-concurrency-race OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" RAG_TEST_TIMEOUT="$${RAG_TEST_TIMEOUT:-3m}"
	$(MAKE) test-ops-integration OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" OPS_TEST_REDIS_ADDR="$(OPS_TEST_REDIS_ADDR)"
	$(MAKE) test-approval-integration OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)"
	$(MAKE) test-rate-limit-integration OPS_TEST_REDIS_ADDR="$(OPS_TEST_REDIS_ADDR)"

.PHONY: test-ops-integration
test-ops-integration: ## Run Ops lease/CAS integration tests (requires MySQL)
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	@test -n "$(OPS_TEST_REDIS_ADDR)" || (echo "OPS_TEST_REDIS_ADDR is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" OPS_TEST_REDIS_ADDR="$(OPS_TEST_REDIS_ADDR)" $(GO) test -tags=integration ./internal/repository/... ./internal/orchestrator/task/... -count=1 -v

.PHONY: test-approval-integration
test-approval-integration: ## Run approval expiry/CAS integration tests (requires MySQL)
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" $(GO) test -tags=integration ./internal/repository/... -run TestApproval -count=1 -v

.PHONY: test-index-integration
test-index-integration: ## Run Index task lease/CAS integration tests (requires MySQL and Redis)
	@test -n "$(OPS_TEST_MYSQL_DSN)" || (echo "OPS_TEST_MYSQL_DSN is required" && exit 2)
	@test -n "$(OPS_TEST_REDIS_ADDR)" || (echo "OPS_TEST_REDIS_ADDR is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_MYSQL_DSN="$(OPS_TEST_MYSQL_DSN)" OPS_TEST_REDIS_ADDR="$(OPS_TEST_REDIS_ADDR)" $(GO) test -tags=integration ./internal/repository/... ./internal/orchestrator/task/... -run TestIndex -count=1 -v

.PHONY: test-rate-limit-integration
test-rate-limit-integration: ## Run Redis sliding-window rate-limit integration tests
	@test -n "$(OPS_TEST_REDIS_ADDR)" || (echo "OPS_TEST_REDIS_ADDR is required" && exit 2)
	GF_GCFG_PATH="$(CURDIR)/manifest/config" OPS_TEST_REDIS_ADDR="$(OPS_TEST_REDIS_ADDR)" $(GO) test -tags=integration ./internal/gateway/middleware/... -run TestSlidingWindow -count=1 -v

.PHONY: test-verbose
test-verbose: ## Run unit tests with verbose output
	$(GO) test ./api/... ./cmd/... ./internal/... -count=1 -short -v

.PHONY: test-coverage
test-coverage: ## Run unit tests and generate coverage report
	@mkdir -p $(OUT_DIR)
	$(GO) test ./api/... ./cmd/... ./internal/... -count=1 -short -coverprofile=$(OUT_DIR)/coverage.out
	$(GO) tool cover -html=$(OUT_DIR)/coverage.out -o $(OUT_DIR)/coverage.html
	@echo "Coverage report: $(OUT_DIR)/coverage.html"

.PHONY: typecheck-frontend
typecheck-frontend: ## Run the Portal TypeScript type check
	cd portal && ./node_modules/.bin/tsc -b --pretty false

.PHONY: test-sensitive-sinks
test-sensitive-sinks: ## Reject direct sensitive values at known log sinks
	bash scripts/check_sensitive_sinks.sh

.PHONY: test-boundaries
test-boundaries: ## Run security boundary regression tests for API, trace and redaction
	$(GO) test ./internal/gateway/handler ./internal/gateway/middleware ./internal/pkg/redact ./internal/toolkit -count=1

.PHONY: build-frontend
build-frontend: ## Build the Portal production bundle
	cd portal && npm run build

.PHONY: verify
verify: test typecheck-frontend test-sensitive-sinks ## Run the repository's offline quality gate

.PHONY: agent-eval-smoke
agent-eval-smoke: ## Run a live Agent evaluation smoke suite (requires a running platform)
	@test -n "$(EVAL_BASE_URL)" || (echo "EVAL_BASE_URL is required, e.g. http://127.0.0.1:8090/api/v1" && exit 2)
	@python3 scripts/run_agent_eval.py --base-url "$(EVAL_BASE_URL)" --api-key "$(EVAL_API_KEY)" --limit "$${EVAL_LIMIT:-3}" --timeout "$${EVAL_TIMEOUT:-120}" --max-iterations "$${EVAL_MAX_ITERATIONS:-10}" --output "$${EVAL_OUTPUT:-/tmp/wisesentinel-agent-eval-smoke.csv}" --summary-json "$${EVAL_SUMMARY:-/tmp/wisesentinel-agent-eval-smoke-summary.json}"

.PHONY: agent-load-chat
agent-load-chat: ## Run cleanup-safe concurrent live Chat requests (requires a running platform)
	@test -n "$(EVAL_BASE_URL)" || (echo "EVAL_BASE_URL is required, e.g. http://127.0.0.1:8090/api/v1" && exit 2)
	@test -n "$(EVAL_API_KEY)" || (echo "EVAL_API_KEY is required" && exit 2)
	@python3 scripts/run_concurrent_chat_load.py --base-url "$(EVAL_BASE_URL)" --api-key "$(EVAL_API_KEY)" --tenant-id "$${LOAD_TENANT_ID:-load-eval}" --requests "$${LOAD_REQUESTS:-4}" --concurrency "$${LOAD_CONCURRENCY:-2}" --timeout "$${LOAD_TIMEOUT:-120}" --min-success-rate "$${LOAD_MIN_SUCCESS_RATE:-1}" --max-p95-ms "$${LOAD_MAX_P95_MS:-0}" --summary-json "$${LOAD_SUMMARY:-/tmp/wisesentinel-chat-load-summary.json}"

.PHONY: agent-sse-contract
agent-sse-contract: ## Run live SSE basic/cancel/delete-race/capacity contract (requires a running platform)
	@test -n "$(EVAL_BASE_URL)" || (echo "EVAL_BASE_URL is required, e.g. http://127.0.0.1:8090/api/v1" && exit 2)
	@test -n "$(EVAL_API_KEY)" || (echo "EVAL_API_KEY is required" && exit 2)
	@python3 scripts/run_sse_chat_contract.py --base-url "$(EVAL_BASE_URL)" --api-key "$(EVAL_API_KEY)" --tenant-id "$${SSE_TENANT_ID:-default}" --scenario "$${SSE_SCENARIO:-basic}" --requests "$${SSE_REQUESTS:-10}" --timeout "$${SSE_TIMEOUT:-120}"

.PHONY: agent-idempotency-contract
agent-idempotency-contract: ## Run live synchronous Chat idempotency contract (requires a running platform)
	@test -n "$(EVAL_BASE_URL)" || (echo "EVAL_BASE_URL is required, e.g. http://127.0.0.1:8090/api/v1" && exit 2)
	@test -n "$(EVAL_API_KEY)" || (echo "EVAL_API_KEY is required" && exit 2)
	@python3 scripts/run_chat_idempotency_contract.py --base-url "$(EVAL_BASE_URL)" --api-key "$(EVAL_API_KEY)" --tenant-id "$${IDEMPOTENCY_TENANT_ID:-default}" --timeout "$${IDEMPOTENCY_TIMEOUT:-120}" --scenario "$${IDEMPOTENCY_SCENARIO:-replay}"

.PHONY: agent-trace-access-contract
agent-trace-access-contract: ## Run live owner-scoped Trace access contract (requires a running platform)
	@test -n "$(EVAL_BASE_URL)" || (echo "EVAL_BASE_URL is required, e.g. http://127.0.0.1:8090/api/v1" && exit 2)
	@test -n "$(EVAL_API_KEY)" || (echo "EVAL_API_KEY is required" && exit 2)
	@python3 scripts/run_trace_access_contract.py --base-url "$(EVAL_BASE_URL)" --api-key "$(EVAL_API_KEY)" --timeout "$${TRACE_TIMEOUT:-120}"

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

.PHONY: migrate
migrate: ## Apply idempotent MySQL migrations through the Compose migration job
	docker compose -f manifest/docker/docker-compose.yml run --rm migrate

.PHONY: test-migration-ledger
test-migration-ledger: ## Validate migration checksum ledger with local Compose MySQL
	bash scripts/test_migration_ledger.sh

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
