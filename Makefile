SHELL := /bin/bash

GO := go
PORT ?= 5789
STATE_FILE_PATH ?= data/kkachi_state.json
E2E_BASE_URL ?=

DAEMON_CMD := ./cmd/sanhod
DAEMON_BINARY := bin/sanhod
CLI_CMD := ./cmd/sanho
CLI_BINARY := bin/sanho

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

.PHONY: \
	daemon-build daemon-run daemon-run-dev \
	daemon-test-prepare daemon-test-unit daemon-test-integration daemon-test-e2e daemon-test \
	cli-build cli-install cli-test-prepare cli-test-unit cli-test-integration cli-test-e2e cli-test \
	docs-check test-all \
	build-daemon-binary run-daemon-local run-daemon-dev-local \
	build-cli install-cli \
	test-daemon-prepare test-daemon-unit test-daemon-int test-daemon-e2e test-daemon \
	test-cli-prepare test-cli-unit test-cli-int test-cli-e2e test-cli \
	check-github-ssh install-launchagent status-launchagent uninstall-launchagent

# ---- Daemon ----

daemon-build:
	@mkdir -p bin
	$(GO) build -o $(DAEMON_BINARY) $(DAEMON_CMD)

daemon-run: daemon-build
	@mkdir -p "$(dir $(STATE_FILE_PATH))"
	PORT=$(PORT) STATE_FILE_PATH=$(STATE_FILE_PATH) ./$(DAEMON_BINARY)

daemon-run-dev:
	@mkdir -p "$(dir $(STATE_FILE_PATH))"
	PORT=$(PORT) STATE_FILE_PATH=$(STATE_FILE_PATH) $(GO) run $(DAEMON_CMD)

daemon-test-prepare:
	@mkdir -p data
	$(GO) generate ./...
	$(GO) fmt ./...
	$(GO) vet ./...

daemon-test-unit:
	$(GO) test ./cmd/... ./internal/...

daemon-test-integration:
	$(GO) test ./test/integration -count=1

daemon-test-e2e: daemon-build
	@if [ -n "$(strip $(E2E_BASE_URL))" ]; then \
		KKACHI_SERVER_BINARY="$(CURDIR)/$(DAEMON_BINARY)" KKACHI_E2E_BASE_URL="$(E2E_BASE_URL)" $(GO) test ./test/e2e -count=1; \
	else \
		KKACHI_SERVER_BINARY="$(CURDIR)/$(DAEMON_BINARY)" $(GO) test ./test/e2e -count=1; \
	fi

daemon-test: daemon-test-prepare daemon-test-unit daemon-test-integration daemon-test-e2e

# ---- CLI ----

cli-build:
	@mkdir -p bin
	$(GO) build $(LDFLAGS) -o $(CLI_BINARY) $(CLI_CMD)

cli-install:
	$(GO) build $(LDFLAGS) -o $(shell $(GO) env GOPATH)/bin/sanho $(CLI_CMD)

cli-test-prepare: cli-build
	$(GO) fmt ./internal/interface/cli/...
	$(GO) vet ./internal/interface/cli/...

cli-test-unit:
	$(GO) test ./internal/interface/cli/...

cli-test-integration: cli-build
	KKACHI_CLI_BINARY=$(CURDIR)/$(CLI_BINARY) $(GO) test ./test/cli/integration -count=1 -v

cli-test-e2e: cli-build daemon-build
	@if [ -n "$(strip $(E2E_BASE_URL))" ]; then \
		KKACHI_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" KKACHI_SERVER_BINARY="$(CURDIR)/$(DAEMON_BINARY)" KKACHI_E2E_BASE_URL="$(E2E_BASE_URL)" $(GO) test ./test/cli/e2e -count=1 -v; \
	else \
		KKACHI_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" KKACHI_SERVER_BINARY="$(CURDIR)/$(DAEMON_BINARY)" $(GO) test ./test/cli/e2e -count=1 -v; \
	fi

cli-test: cli-test-prepare cli-test-unit cli-test-integration cli-test-e2e

docs-check:
	@test -f README.md
	@test -f docs/architecture.md
	@test -f docs/operations.md
	@test -f docs/readme/kor.md
	@if grep -REn 'docs/requirement\.md|build-server-with-web|run-web-local|run-local-dev-with-web|WEB_DIST_DIR|PTY_' README.md docs; then \
		echo "Error: stale documentation reference found."; \
		exit 1; \
	fi

test-all: docs-check daemon-test cli-test

# Compatibility aliases for the previous target names.
build-daemon-binary: daemon-build
run-daemon-local: daemon-run
run-daemon-dev-local: daemon-run-dev
build-cli: cli-build
install-cli: cli-install
test-daemon-prepare: daemon-test-prepare
test-daemon-unit: daemon-test-unit
test-daemon-int: daemon-test-integration
test-daemon-e2e: daemon-test-e2e
test-daemon: daemon-test
test-cli-prepare: cli-test-prepare
test-cli-unit: cli-test-unit
test-cli-int: cli-test-integration
test-cli-e2e: cli-test-e2e
test-cli: cli-test

# ---- LaunchAgent (macOS) ----

LAUNCH_LABEL := com.seventeenthearth.kkachi
PLIST_NAME := $(LAUNCH_LABEL).plist
PLIST_TEMPLATE := $(CURDIR)/$(PLIST_NAME).template
PLIST_DST := $(HOME)/Library/LaunchAgents/$(PLIST_NAME)
LOG_DIR := $(HOME)/Library/Logs/kkachi
LAUNCH_DOMAIN := gui/$(shell id -u)
LAUNCH_SERVICE := $(LAUNCH_DOMAIN)/$(LAUNCH_LABEL)

check-github-ssh:
	@echo "=== Checking GitHub SSH access ==="
	@GIT_SSH_COMMAND='ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new' \
		git ls-remote origin HEAD >/dev/null
	@echo "GitHub SSH access confirmed."

install-launchagent: check-github-ssh daemon-build
	@echo "=== Installing sanho LaunchAgent ==="
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
	@echo "Verify with: make status-launchagent"

status-launchagent:
	@launchctl print "$(LAUNCH_SERVICE)"

uninstall-launchagent:
	@echo "=== Uninstalling sanho LaunchAgent ==="
	@launchctl bootout "$(LAUNCH_SERVICE)" 2>/dev/null || true
	@rm -f "$(PLIST_DST)"
	@echo "LaunchAgent uninstalled."
