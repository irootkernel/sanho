SHELL := /bin/bash
GO := go
PORT ?= 5789
STATE_FILE_PATH ?= data/kkachi_state.json
E2E_BASE_URL ?= http://127.0.0.1:5789
SERVER_CMD := ./cmd/server
DOCKER_IMAGE ?= kkachi-server
DOCKER_IMAGE_DEV ?= $(DOCKER_IMAGE):dev
DOCKER_CONTAINER_NAME ?= kkachi-server
# Mount host temp so in-container git can see host-created temp repos (e2e). Auto-add /var/folders on macOS.
EXTRA_TMP_MOUNT ?= $(shell if [ -d /var/folders ]; then echo "-v /var/folders:/var/folders"; fi)

.PHONY: server-test-prepare server-test-unit server-test-integration server-test-e2e server-test server-run server-build

# Generate any code stubs, format, and run basic lint checks.
server-test-prepare:
	mkdir -p data
	$(GO) generate ./...
	$(GO) fmt ./...
	$(GO) vet ./...

# Run fast/unit-level tests (excludes e2e package).
server-test-unit:
	$(GO) test ./cmd/... ./internal/...

# In-process HTTP server + fake git repos.
server-test-integration:
	$(GO) test ./test/integration -count=1

# Run end-to-end tests against a real kkachi-server process.
server-test-e2e:
	KKACHI_E2E_BASE_URL=$(E2E_BASE_URL) $(GO) test ./test/e2e -count=1

# Full test pipeline.
server-test: server-test-prepare server-test-unit server-test-integration server-test-e2e

# Launch the server with optional PORT and STATE_FILE_PATH overrides.
server-run:
	docker build --target dev -t $(DOCKER_IMAGE_DEV) .
	docker rm -f $(DOCKER_CONTAINER_NAME) >/dev/null 2>&1 || true
	docker run --rm -it \
		--name $(DOCKER_CONTAINER_NAME) \
		-p $(PORT):$(PORT) \
		-e PORT=$(PORT) \
		-e STATE_FILE_PATH=$(STATE_FILE_PATH) \
		-v $(CURDIR):/app \
		-v /tmp:/tmp \
		$(EXTRA_TMP_MOUNT) \
		$(DOCKER_IMAGE_DEV)

# Build a production image.
server-build:
	docker build -t $(DOCKER_IMAGE) .

# ---- CLI Targets ----

CLI_CMD := ./cmd/kkachi
CLI_BINARY := bin/kkachi
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

.PHONY: cli-build cli-install cli-test cli-test-unit cli-test-integration cli-test-e2e cli-test-prepare

# Build the kkachi CLI binary.
cli-build:
	mkdir -p bin
	$(GO) build $(LDFLAGS) -o $(CLI_BINARY) $(CLI_CMD)

# Install the kkachi CLI to $GOPATH/bin.
cli-install:
	$(GO) install $(LDFLAGS) $(CLI_CMD)

# Prepare for CLI tests (build binary first).
cli-test-prepare: cli-build
	$(GO) fmt ./internal/interface/cli/...
	$(GO) vet ./internal/interface/cli/...

# Run CLI unit tests (in-package tests).
cli-test-unit:
	$(GO) test ./internal/interface/cli/...

# Run CLI integration tests (CLI binary + fake server/temp dirs).
cli-test-integration: cli-build
	KKACHI_CLI_BINARY=$(CURDIR)/$(CLI_BINARY) $(GO) test ./test/cli/integration -count=1 -v

# Run CLI end-to-end tests (CLI binary + real kkachi-server).
cli-test-e2e: cli-build
	KKACHI_CLI_BINARY=$(CURDIR)/$(CLI_BINARY) KKACHI_E2E_BASE_URL=$(E2E_BASE_URL) $(GO) test ./test/cli/e2e -count=1 -v

# Full CLI test pipeline.
cli-test: cli-test-prepare cli-test-unit cli-test-integration cli-test-e2e

