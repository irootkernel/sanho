SHELL := /bin/bash

GO := go
SANHO_HOME ?=
SANHO_SOCKET ?=
E2E_SOCKET ?=

DAEMON_CMD := ./cmd/sanhod
DAEMON_BINARY := bin/sanhod
CLI_CMD := ./cmd/sanho
CLI_BINARY := bin/sanho

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"
DAEMON_LDFLAGS := -ldflags "-X main.version=$(VERSION)"

DAEMON_UNIT_PACKAGES := \
	./cmd/sanhod \
	./internal/config \
	./internal/domain \
	./internal/domain/docs \
	./internal/domain/workspace \
	./internal/infra/git \
	./internal/infra/state \
	./internal/interface/http/... \
	./internal/usecase \
	./internal/usecase/docs \
	./internal/usecase/project \
	./internal/usecase/state \
	./internal/usecase/workspace

CLIENT_UNIT_PACKAGES := \
	./cmd/sanho \
	./internal/buildinfo \
	./internal/domain/client \
	./internal/domain/merge \
	./internal/infra/fs \
	./internal/infra/httpclient \
	./internal/interface/cli \
	./internal/usecase/hook

DAEMON_CHECK_PACKAGES := $(DAEMON_UNIT_PACKAGES) ./test/integration ./test/e2e
CLIENT_CHECK_PACKAGES := $(CLIENT_UNIT_PACKAGES) ./test/cli/integration ./test/cli/e2e ./test/install

.PHONY: \
	daemon-build daemon-install daemon-run daemon-run-dev \
	cli-build cli-install install docs-check test-package-ownership test-architecture \
	test test-prepare test-prepare-daemon test-prepare-client \
	test-unit test-unit-daemon test-unit-client \
	test-int test-int-daemon test-int-client \
	test-e2e test-e2e-daemon test-e2e-client \
	build-daemon-binary run-daemon-local run-daemon-dev-local \
	build-cli install-cli

# ---- Daemon ----

daemon-build:
	@mkdir -p bin
	$(GO) build $(DAEMON_LDFLAGS) -o $(DAEMON_BINARY) $(DAEMON_CMD)

daemon-install:
	$(GO) install $(DAEMON_LDFLAGS) $(DAEMON_CMD)

daemon-run: daemon-build
	SANHO_HOME="$(SANHO_HOME)" SANHO_SOCKET="$(SANHO_SOCKET)" ./$(DAEMON_BINARY)

daemon-run-dev:
	SANHO_HOME="$(SANHO_HOME)" SANHO_SOCKET="$(SANHO_SOCKET)" $(GO) run $(DAEMON_CMD)

# ---- CLI ----

cli-build:
	@mkdir -p bin
	$(GO) build $(LDFLAGS) -o $(CLI_BINARY) $(CLI_CMD)

cli-install:
	$(GO) install $(LDFLAGS) $(CLI_CMD)

docs-check:
	@test -f README.md
	@test -f CHANGELOG.md
	@test -f docs/architecture.md
	@test -f docs/cli-json.md
	@test -f docs/deployment.md
	@test -f docs/hands-on-testing.md
	@test -f docs/operations.md
	@test -f docs/recovery.md
	@test -f docs/readme/kor.md
	@if grep -REn 'docs/requirement\.md|build-server-with-web|run-web-local|run-local-dev-with-web|WEB_DIST_DIR|PTY_' README.md docs; then \
		echo "Error: stale documentation reference found."; \
		exit 1; \
	fi

install: daemon-install cli-install

# ---- Tests ----

test:
	$(MAKE) test-prepare
	$(MAKE) test-unit
	$(MAKE) test-int
	$(MAKE) test-e2e

test-prepare:
	@mkdir -p data
	$(GO) generate ./...
	$(GO) fmt ./...
	$(GO) mod verify
	$(MAKE) docs-check
	$(MAKE) test-package-ownership
	$(MAKE) test-architecture
	$(MAKE) test-prepare-daemon
	$(MAKE) test-prepare-client

test-prepare-daemon:
	$(GO) vet $(DAEMON_CHECK_PACKAGES)
	$(GO) tool golangci-lint run $(DAEMON_CHECK_PACKAGES)

test-prepare-client:
	$(GO) vet $(CLIENT_CHECK_PACKAGES)
	$(GO) tool golangci-lint run $(CLIENT_CHECK_PACKAGES)

test-package-ownership:
	@set -euo pipefail; \
	actual_file="$$(mktemp)"; \
	daemon_file="$$(mktemp)"; \
	client_file="$$(mktemp)"; \
	owned_file="$$(mktemp)"; \
	trap 'rm -f "$$actual_file" "$$daemon_file" "$$client_file" "$$owned_file"' EXIT; \
	$(GO) list ./cmd/... ./internal/... | grep -v '/internal/architecture$$' | sort > "$$actual_file"; \
	$(GO) list $(DAEMON_UNIT_PACKAGES) | sort > "$$daemon_file"; \
	$(GO) list $(CLIENT_UNIT_PACKAGES) | sort > "$$client_file"; \
	if comm -12 "$$daemon_file" "$$client_file" | grep -q .; then \
		echo "Error: packages assigned to both daemon and client:"; \
		comm -12 "$$daemon_file" "$$client_file"; \
		exit 1; \
	fi; \
	sort -u "$$daemon_file" "$$client_file" > "$$owned_file"; \
	if ! diff -u "$$actual_file" "$$owned_file"; then \
		echo "Error: daemon/client unit package ownership is incomplete."; \
		exit 1; \
	fi

test-architecture:
	$(GO) vet ./internal/architecture
	$(GO) tool golangci-lint run ./internal/architecture
	$(GO) test ./internal/architecture -count=1

test-unit:
	$(MAKE) test-unit-daemon
	$(MAKE) test-unit-client

test-unit-daemon:
	$(GO) test $(DAEMON_UNIT_PACKAGES)

test-unit-client:
	$(GO) test $(CLIENT_UNIT_PACKAGES)

test-int:
	$(MAKE) test-int-daemon
	$(MAKE) test-int-client

test-int-daemon:
	$(GO) test ./test/integration -count=1

test-int-client: cli-build
	SANHO_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" $(GO) test ./test/cli/integration -count=1 -v

test-e2e:
	$(MAKE) test-e2e-daemon
	$(MAKE) test-e2e-client

test-e2e-daemon: daemon-build
	@if [ -n "$(strip $(E2E_SOCKET))" ]; then \
		SANHO_DAEMON_BINARY="$(CURDIR)/$(DAEMON_BINARY)" SANHO_E2E_SOCKET="$(E2E_SOCKET)" $(GO) test ./test/e2e -count=1; \
	else \
		SANHO_DAEMON_BINARY="$(CURDIR)/$(DAEMON_BINARY)" $(GO) test ./test/e2e -count=1; \
	fi

test-e2e-client: cli-build daemon-build
	$(GO) test ./test/install -count=1
	@if [ -n "$(strip $(E2E_SOCKET))" ]; then \
		SANHO_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" SANHO_DAEMON_BINARY="$(CURDIR)/$(DAEMON_BINARY)" SANHO_E2E_SOCKET="$(E2E_SOCKET)" $(GO) test ./test/cli/e2e -count=1 -v; \
	else \
		SANHO_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" SANHO_DAEMON_BINARY="$(CURDIR)/$(DAEMON_BINARY)" $(GO) test ./test/cli/e2e -count=1 -v; \
	fi

# Compatibility aliases for the previous target names.
build-daemon-binary: daemon-build
run-daemon-local: daemon-run
run-daemon-dev-local: daemon-run-dev
build-cli: cli-build
install-cli: cli-install
