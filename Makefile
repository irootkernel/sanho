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
	@echo "  Web UI: http://localhost:$(WEB_DEV_PORT)"
	@echo ""
	@cd $(WEB_DIR) && WEB_DEV_PORT=$(WEB_DEV_PORT) npm run dev

# Run server with web dev server (local, no Docker)
# This starts server binary and web dev server in separate processes
run-local-dev-with-web:
	@echo "=== Starting kkachi-server + web dev server (Docker-free) ==="
	@echo "  Server API: http://localhost:$${PORT:-5789}"
	@echo "  Web UI:     http://localhost:$(WEB_DEV_PORT)"
	@echo ""
	@mkdir -p data
	@# Build web first for production serving
	@if [ ! -d "$(WEB_DIST_DIR)" ]; then \
		echo "Building web..."; \
		$(MAKE) build-web; \
	fi
	@# Build server binary if not present
	@if [ ! -f "./bin/server" ]; then \
		$(MAKE) build-server-binary; \
	fi
	@if [ ! -x "$(WEB_DIR)/node_modules/.bin/vite" ]; then \
		cd $(WEB_DIR) && npm ci; \
	fi
	@# Start and supervise both processes so launchd can observe either failure.
	@set -u; \
	SERVER_PID=""; \
	WEB_PID=""; \
	cleanup() { \
		status=$$?; \
		trap - EXIT INT TERM; \
		if [ -n "$$SERVER_PID" ]; then kill "$$SERVER_PID" 2>/dev/null || true; fi; \
		if [ -n "$$WEB_PID" ]; then kill "$$WEB_PID" 2>/dev/null || true; fi; \
		if [ -n "$$SERVER_PID" ]; then wait "$$SERVER_PID" 2>/dev/null || true; fi; \
		if [ -n "$$WEB_PID" ]; then wait "$$WEB_PID" 2>/dev/null || true; fi; \
		exit "$$status"; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	WEB_DIST_DIR=$(WEB_DIST_DIR) ./bin/server & \
	SERVER_PID=$$!; \
	(cd $(WEB_DIR) && exec env WEB_DEV_PORT=$(WEB_DEV_PORT) ./node_modules/.bin/vite) & \
	WEB_PID=$$!; \
	echo "Server PID: $$SERVER_PID"; \
	echo "Web PID:    $$WEB_PID"; \
	echo ""; \
	echo "Press Ctrl+C to stop both servers"; \
	echo ""; \
	while kill -0 "$$SERVER_PID" 2>/dev/null && kill -0 "$$WEB_PID" 2>/dev/null; do \
		sleep 1; \
	done; \
	status=1; \
	if ! kill -0 "$$SERVER_PID" 2>/dev/null; then \
		wait "$$SERVER_PID"; \
		status=$$?; \
	else \
		wait "$$WEB_PID"; \
		status=$$?; \
	fi; \
	if [ "$$status" -eq 0 ]; then status=1; fi; \
	exit "$$status"

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
WEB_DEV_PORT ?= 5790

.PHONY: run-web run-server-with-web check-web build-web build-server-binary build-server-with-web

# Run web dev server (local, requires Node.js)
run-web:
	@echo "Starting web dev server..."
	cd $(WEB_DIR) && WEB_DEV_PORT=$(WEB_DEV_PORT) npm run dev

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
	cd $(WEB_DIR) && npm ci && npm run build
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
# Note: E2E tests expect kkachi-server on port 5789 and web on port 5790
test-web-e2e: test-web-prepare
	cd $(WEB_DIR) && npm run test:e2e

# Full web test pipeline
.NOTPARALLEL: test-web
test-web: test-web-prepare test-web-unit test-web-int test-web-e2e

# ---- LaunchAgent Targets ----

LAUNCH_LABEL := com.seventeenthearth.kkachi
PLIST_NAME := $(LAUNCH_LABEL).plist
PLIST_TEMPLATE := $(CURDIR)/$(PLIST_NAME).template
PLIST_DST := $(HOME)/Library/LaunchAgents/$(PLIST_NAME)
LOG_DIR := $(HOME)/Library/Logs/kkachi
LAUNCH_DOMAIN := gui/$(shell id -u)
LAUNCH_SERVICE := $(LAUNCH_DOMAIN)/$(LAUNCH_LABEL)

.PHONY: check-github-ssh install-launchagent status-launchagent uninstall-launchagent

# Verify that the current repository origin is reachable with non-interactive SSH.
check-github-ssh:
	@echo "=== Checking GitHub SSH access ==="
	@GIT_SSH_COMMAND='ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new' \
		git ls-remote origin HEAD >/dev/null
	@echo "GitHub SSH access confirmed."

# Install LaunchAgent for auto-start on login
install-launchagent: check-github-ssh build-server-with-web
	@echo "=== Installing kkachi LaunchAgent ==="
	@if [ "$$(uname -s)" != "Darwin" ]; then \
		echo "Error: LaunchAgent installation is supported only on macOS."; \
		exit 1; \
	fi
	@mkdir -p "$(LOG_DIR)"
	@mkdir -p "$(HOME)/Library/LaunchAgents"
	@tmp_plist="$$(mktemp "$${TMPDIR:-/tmp}/kkachi-launchagent.XXXXXX")"; \
	trap 'rm -f "$$tmp_plist"' EXIT; \
	cp "$(PLIST_TEMPLATE)" "$$tmp_plist"; \
	plutil -replace Program -string "$(CURDIR)/run-kkachi.sh" "$$tmp_plist"; \
	plutil -replace WorkingDirectory -string "$(CURDIR)" "$$tmp_plist"; \
	plutil -replace StandardOutPath -string "$(LOG_DIR)/kkachi.out.log" "$$tmp_plist"; \
	plutil -replace StandardErrorPath -string "$(LOG_DIR)/kkachi.err.log" "$$tmp_plist"; \
	plutil -lint "$$tmp_plist"; \
	old_pid="$$(launchctl print "$(LAUNCH_SERVICE)" 2>/dev/null | awk '$$1 == "pid" { print $$3; exit }')"; \
	launchctl bootout "$(LAUNCH_SERVICE)" 2>/dev/null || true; \
	if [ -n "$$old_pid" ]; then \
		attempt=0; \
		while kill -0 "$$old_pid" 2>/dev/null && [ "$$attempt" -lt 50 ]; do \
			sleep 0.1; \
			attempt=$$((attempt + 1)); \
		done; \
		if kill -0 "$$old_pid" 2>/dev/null; then \
			echo "Error: previous LaunchAgent process did not stop: $$old_pid"; \
			exit 1; \
		fi; \
	fi; \
	sleep 1; \
	install -m 0644 "$$tmp_plist" "$(PLIST_DST)"; \
	launchctl bootstrap "$(LAUNCH_DOMAIN)" "$(PLIST_DST)"; \
	launchctl kickstart -k "$(LAUNCH_SERVICE)"
	@echo "LaunchAgent installed and started."
	@echo "  Logs: $(LOG_DIR)/"
	@echo ""
	@echo "Verify with: make status-launchagent"

# Print LaunchAgent status.
status-launchagent:
	@launchctl print "$(LAUNCH_SERVICE)"

# Uninstall LaunchAgent
uninstall-launchagent:
	@echo "=== Uninstalling kkachi LaunchAgent ==="
	@launchctl bootout "$(LAUNCH_SERVICE)" 2>/dev/null || true
	@rm -f "$(PLIST_DST)"
	@echo "LaunchAgent uninstalled."
