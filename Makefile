SHELL := /bin/bash
GO := go
PORT ?= 5789
STATE_FILE_PATH ?= data/kkachi_state.json
E2E_BASE_URL ?= http://127.0.0.1:5789
SERVER_CMD := ./cmd/server

.PHONY: test-server-prepare test-server-unit test-server-int test-server-e2e test-server run-server build-server

.PHONY: test-all run-server-local run-server-dev-local run-web-local run-local-dev-with-web

# Run server + CLI test pipelines.
test-all: test-server test-cli

# Generate any code stubs, format, and run basic lint checks.
test-server-prepare:
	mkdir -p data
	$(GO) generate ./...
	$(GO) fmt ./...
	$(GO) vet ./...

# Run fast/unit-level tests (excludes e2e package).
test-server-unit:
	$(GO) test ./cmd/... ./internal/...

# In-process HTTP server + fake git repos.
test-server-int:
	$(GO) test ./test/integration -count=1

# Run end-to-end tests against a real kkachi-server process.
test-server-e2e:
	KKACHI_E2E_BASE_URL=$(E2E_BASE_URL) $(GO) test ./test/e2e -count=1

# Full test pipeline.
test-server: test-server-prepare test-server-unit test-server-int test-server-e2e

# ---- Local Server Development Targets ----

# Run server locally (production binary)
run-server-local: build-server-binary
	@echo "=== Starting server (production binary) ==="
	@echo "  Server API: http://localhost:$${PORT:-5789}"
	@echo "  Web UI:     http://localhost:$${PORT:-5789}"
	@echo ""
	@if [ ! -d "$(WEB_DIST_DIR)" ]; then \
		echo "Warning: $(WEB_DIST_DIR) not found. Web UI will not be available."; \
		echo "Run 'make build-web' to build web UI."; \
	fi
	@mkdir -p data
	@WEB_DIST_DIR=$(WEB_DIST_DIR) ./bin/server

# Run server locally with hot reload (requires air)
# Note: Install air first: go install github.com/air-verse/air@latest
run-server-dev-local:
	@echo "=== Starting server with hot reload (air) ==="
	@echo "  Server API: http://localhost:$${PORT:-5789}"
	@echo "  Web UI:     http://localhost:$${PORT:-5789}"
	@echo ""
	@mkdir -p data
	@if [ ! -d "$(WEB_DIST_DIR)" ]; then \
		echo "Building web first..."; \
		$(MAKE) build-web; \
	fi
	@WEB_DIST_DIR=$(WEB_DIST_DIR) air

# Start web dev server only
run-web-local:
	@echo "=== Starting web dev server ==="
	@echo "  Web UI: http://localhost:5173"
	@echo ""
	@cd $(WEB_DIR) && npm run dev

# Run server with web dev server (local, no Docker)
# This starts server binary and web dev server in separate processes
run-local-dev-with-web:
	@echo "=== Starting kkachi-server + web dev server (Docker-free) ==="
	@echo "  Server API: http://localhost:$${PORT:-5789}"
	@echo "  Web UI:     http://localhost:5173"
	@echo ""
	@mkdir -p data
	@# Build web first for production serving
	@if [ ! -d "$(WEB_DIST_DIR)" ]; then \
		echo "Building web..."; \
		$(MAKE) build-web; \
	fi
	@# Start server in background, capture PID, and start web dev server
	@trap 'kill $$$$! 2>/dev/null || true' EXIT; \
	WEB_DIST_DIR=$(WEB_DIST_DIR) ./bin/server & \
	SERVER_PID=$$$$!; \
	echo "Server PID: $$$$SERVER_PID"; \
	echo ""; \
	echo "Press Ctrl+C to stop both servers"; \
	echo ""; \
	cd $(WEB_DIR) && npm run dev

# ---- CLI Targets ----

CLI_CMD := ./cmd/kkachi
CLI_BINARY := bin/kkachi
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

.PHONY: build-cli install-cli test-cli test-cli-prepare test-cli-unit test-cli-int test-cli-e2e

# Build the kkachi CLI binary.
build-cli:
	mkdir -p bin
	$(GO) build $(LDFLAGS) -o $(CLI_BINARY) $(CLI_CMD)

# Install the kkachi CLI to $GOPATH/bin.
install-cli:
	$(GO) build $(LDFLAGS) -o $(shell $(GO) env GOPATH)/bin/kkachi-cli $(CLI_CMD)

# Prepare for CLI tests (build binary first).
test-cli-prepare: build-cli
	$(GO) fmt ./internal/interface/cli/...
	$(GO) vet ./internal/interface/cli/...

# Run CLI unit tests (in-package tests).
test-cli-unit:
	$(GO) test ./internal/interface/cli/...

# Run CLI integration tests (CLI binary + fake server/temp dirs).
test-cli-int: build-cli
	KKACHI_CLI_BINARY=$(CURDIR)/$(CLI_BINARY) $(GO) test ./test/cli/integration -count=1 -v

# Run CLI end-to-end tests (CLI binary + real kkachi-server).
test-cli-e2e: build-cli
	KKACHI_CLI_BINARY=$(CURDIR)/$(CLI_BINARY) KKACHI_E2E_BASE_URL=$(E2E_BASE_URL) $(GO) test ./test/cli/e2e -count=1 -v

# Full CLI test pipeline.
test-cli: test-cli-prepare test-cli-unit test-cli-int test-cli-e2e

# ---- Web + Server Build Targets (v2) ----

WEB_DIR := web
WEB_DIST_DIR := $(WEB_DIR)/dist

.PHONY: run-web run-server-with-web check-web build-web build-server-binary build-server-with-web

# Run web dev server (local, requires Node.js)
run-web:
	@echo "Starting web dev server..."
	cd $(WEB_DIR) && npm run dev

# Check if web dist exists
check-web:
	@if [ ! -d "$(WEB_DIST_DIR)" ]; then \
		echo "Warning: $(WEB_DIST_DIR) not found. Run 'make build-web' or 'make build-server-with-web' first."; \
		exit 1; \
	fi
	@echo "Web dist found: $(WEB_DIST_DIR)"

# Build web UI (requires Node.js and npm)
build-web:
	@if [ ! -d "$(WEB_DIR)" ]; then \
		echo "Error: $(WEB_DIR)/ directory not found. Web UI source is required."; \
		exit 1; \
	fi
	@echo "Building web UI..."
	cd $(WEB_DIR) && npm run build
	@echo "Web build complete: $(WEB_DIST_DIR)/"

# Build server binary only (without web)
build-server-binary:
	@echo "Building server binary..."
	mkdir -p bin
	$(GO) build -o bin/server $(SERVER_CMD)
	@echo "Server build complete: bin/server"

# Build web + server together (recommended for production deployment)
build-server-with-web: build-web build-server-binary
	@echo ""
	@echo "=== Full build complete ==="
	@echo "  Server binary: bin/server"
	@echo "  Web dist:      $(WEB_DIST_DIR)/"
	@echo ""
	@echo "To run: WEB_DIST_DIR=$(WEB_DIST_DIR) ./bin/server"

# ---- Web Test Targets ----

.PHONY: test-web test-web-prepare test-web-lint test-web-unit test-web-int test-web-e2e

# Run web lint checks
test-web-lint:
	cd $(WEB_DIR) && npm run lint

# Prepare web checks (lint + build before tests)
test-web-prepare: test-web-lint

# Run web unit tests (domain, application pure logic)
test-web-unit: test-web-prepare
	cd $(WEB_DIR) && npm run test:unit

# Run web integration tests (component tests with mocks)
test-web-int: test-web-prepare
	cd $(WEB_DIR) && npm run test:int

# Run web E2E tests (requires server + web running)
# Note: E2E tests expect kkachi-server on port 5789 and web on port 5173
test-web-e2e: test-web-prepare
	cd $(WEB_DIR) && npm run test:e2e

# Full web test pipeline
.NOTPARALLEL: test-web
test-web: test-web-prepare test-web-unit test-web-int test-web-e2e
