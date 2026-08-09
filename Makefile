SHELL := /bin/bash

GO := go
SANHO_HOME ?=

CLI_CMD := ./cmd/sanho
CLI_BINARY := bin/sanho

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

UNIT_PACKAGES := \
	./cmd/sanho \
	./internal/buildinfo \
	./internal/domain/markers \
	./internal/domain/provenance \
	./internal/domain/publish \
	./internal/infra/appgit \
	./internal/infra/canonical \
	./internal/infra/fsx \
	./internal/infra/gitx \
	./internal/infra/registry \
	./internal/infra/wsstate \
	./internal/interface/cli \
	./internal/usecase/admin \
	./internal/usecase/docsync \
	./internal/usecase/publish

CHECK_PACKAGES := $(UNIT_PACKAGES) \
	./test/cli/integration ./test/cli/e2e ./test/install ./test/docsync

.PHONY: \
	cli-build cli-install install docs-check test-package-ownership test-architecture \
	test test-prepare \
	test-unit test-int test-e2e test-scale \
	build-cli install-cli

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
	@if grep -REn '[가-힣]' README.md docs; then \
		echo "Error: repository documentation must be English-only."; \
		exit 1; \
	fi
	@if grep -REn 'sanho-v0\.2\.md' AGENTS.md README.md docs cmd internal test; then \
		echo "Error: deleted design-record reference found."; \
		exit 1; \
	fi
	@for id in 01 02 03 04 05 06 07 08; do \
		grep -Eq "^## H$$id\\." docs/hands-on-testing.md || { echo "Error: missing hands-on H$$id."; exit 1; }; \
	done
	@if grep -Eq '^## H09\.' docs/hands-on-testing.md; then \
		echo "Error: hands-on IDs must end at H08."; \
		exit 1; \
	fi
	@if grep -REn 'docs/requirement\.md|build-server-with-web|run-web-local|run-local-dev-with-web|WEB_DIST_DIR|PTY_' README.md docs; then \
		echo "Error: stale documentation reference found."; \
		exit 1; \
	fi

install: cli-install

# ---- Tests ----

test:
	$(MAKE) test-prepare
	$(MAKE) test-unit
	$(MAKE) test-int
	$(MAKE) test-e2e

test-prepare:
	$(GO) generate ./...
	$(GO) fmt ./...
	$(GO) mod verify
	$(MAKE) docs-check
	$(MAKE) test-package-ownership
	$(MAKE) test-architecture
	$(GO) vet $(CHECK_PACKAGES)
	$(GO) tool golangci-lint run $(CHECK_PACKAGES)

test-package-ownership:
	@set -euo pipefail; \
	actual_file="$$(mktemp)"; \
	want_file="$$(mktemp)"; \
	trap 'rm -f "$$actual_file" "$$want_file"' EXIT; \
	$(GO) list ./cmd/... ./internal/... | grep -v '/internal/architecture$$' | sort > "$$actual_file"; \
	$(GO) list $(UNIT_PACKAGES) | sort > "$$want_file"; \
	if ! diff -u "$$actual_file" "$$want_file"; then \
		echo "Error: unit package ownership is incomplete."; \
		exit 1; \
	fi

test-architecture:
	$(GO) vet ./internal/architecture
	$(GO) tool golangci-lint run ./internal/architecture
	$(GO) test ./internal/architecture -count=1

test-unit:
	$(GO) test $(UNIT_PACKAGES) -race

test-int: cli-build
	SANHO_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" $(GO) test ./test/cli/integration -count=1 -v -race
	$(GO) test ./test/docsync -count=1 -race

# test/cli/e2e is the v0.2 scenario suite restored by P5: the guidance
# guidance-closure table, the scenario matrix, and process-level concurrency.
#
# It runs WITHOUT -race, deliberately. Every assertion here is about
# separate `sanho` and `git` *processes*, so the detector would only
# instrument the test harness that spawns them — buying nothing while
# roughly halving throughput. The in-process suites carry -race
# (test-unit, test-int), which is where it detects anything.
test-e2e: cli-build
	SANHO_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" $(GO) test ./test/cli/e2e -count=1 -v -timeout 20m
	$(GO) test ./test/install -count=1

# Opt-in correctness profile for the large-repository boundary. It is kept
# out of `make test`: timings are evidence, not a release latency threshold.
test-scale: cli-build
	@if [[ "$${SANHO_SCALE:-}" != "1" ]]; then \
		echo "Error: test-scale requires SANHO_SCALE=1."; \
		exit 2; \
	fi
	SANHO_SCALE=1 SANHO_CLI_BINARY="$(CURDIR)/$(CLI_BINARY)" $(GO) test ./test/scale -count=1 -v -timeout 30m

# Compatibility aliases for the previous target names.
build-cli: cli-build
install-cli: cli-install
