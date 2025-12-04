SHELL := /bin/bash
GO := go
PORT ?= 5789
STATE_FILE_PATH ?= data/kkachi_state.json
E2E_BASE_URL ?= http://127.0.0.1:5789
SERVER_CMD := ./cmd/server

.PHONY: server-test-prepare server-test-unit server-test-integration server-test-e2e server-test server-run

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
	PORT=$(PORT) STATE_FILE_PATH=$(STATE_FILE_PATH) $(GO) run $(SERVER_CMD)
