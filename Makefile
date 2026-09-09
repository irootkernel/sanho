SHELL := /bin/bash

GO := go
SANHO_HOME ?=

CLI_CMD := ./cmd/sanho
CLI_BINARY := bin/sanho

VERSION ?=
LDFLAGS := $(if $(strip $(VERSION)),-ldflags "-X main.version=$(strip $(VERSION))")

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
	./test/cli/integration ./test/cli/e2e ./test/install ./test/docsync \
	./test/aquariumdev

.PHONY: \
	cli-build cli-install install docs-check test-package-ownership test-architecture \
	test test-prepare \
	test-unit test-int test-e2e test-scale \
	aquarium-dev-describe aquarium-dev-build aquarium-dev-realconsumer \
	build-cli install-cli

# ---- CLI ----

cli-build:
	@mkdir -p bin
	$(GO) build $(LDFLAGS) -o $(CLI_BINARY) $(CLI_CMD)

cli-install:
	$(GO) install $(LDFLAGS) $(CLI_CMD)

# ---- Aquarium development producer ----

# These targets intentionally stay in Make rather than adding another Go
# executable.  The source version is read from the existing buildinfo
# authority, and the build target archives the admitted Git tree before Go
# sees it.  That keeps ignored and untracked checkout files out of the
# executable while keeping every temporary write below the caller's output.

aquarium-dev-describe:
	@set -euo pipefail; \
		source_version="$$(awk -F'"' '/^[[:space:]]*CurrentVersion[[:space:]]*=[[:space:]]*"/ { print $$2; exit }' internal/buildinfo/version.go)"; \
		if ! printf '%s\n' "$$source_version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$$'; then \
			printf '%s\n' 'aquarium-dev: internal/buildinfo/version.go has no valid CurrentVersion' >&2; \
			exit 1; \
		fi; \
		printf '{"schema":"aquarium-dev-producer-description/v1","project_id":"sanho","next_version":"%s","artifact_kind":"executable","artifact_path":"bin/sanho"}\n' "$$source_version"

aquarium-dev-build:
	@set -euo pipefail; \
		output="$${AQUARIUM_DEV_OUTPUT:-}"; \
		if [[ -z "$$output" || "$${output:0:1}" != / ]]; then \
			printf '%s\n' 'aquarium-dev: AQUARIUM_DEV_OUTPUT must be an absolute path to an empty directory' >&2; \
			exit 1; \
		fi; \
		if [[ ! -d "$$output" || -L "$$output" ]]; then \
			printf '%s\n' 'aquarium-dev: AQUARIUM_DEV_OUTPUT must name an existing empty directory' >&2; \
			exit 1; \
		fi; \
		output="$$(cd -- "$$output" && pwd -P)"; \
		for child in "$$output"/* "$$output"/.[!.]* "$$output"/..?*; do \
			if [[ -e "$$child" || -L "$$child" ]]; then \
				printf '%s\n' 'aquarium-dev: AQUARIUM_DEV_OUTPUT must be empty' >&2; \
				exit 1; \
			fi; \
		done; \
		git_ro() { env \
			-u GIT_DIR \
			-u GIT_WORK_TREE \
			-u GIT_COMMON_DIR \
			-u GIT_INDEX_FILE \
			-u GIT_OBJECT_DIRECTORY \
			-u GIT_ALTERNATE_OBJECT_DIRECTORIES \
			-u GIT_NAMESPACE \
			-u GIT_QUARANTINE_PATH \
			-u GIT_CEILING_DIRECTORIES \
			-u GIT_CONFIG_GLOBAL \
			-u GIT_CONFIG_SYSTEM \
			-u GIT_CONFIG_COUNT \
			-u GIT_CONFIG_PARAMETERS \
			-u GIT_TEMPLATE_DIR \
			-u GIT_TRACE \
			-u GIT_TRACE_PERFORMANCE \
			-u GIT_TRACE_PACKET \
			-u GIT_TRACE2 \
			-u GIT_TRACE2_EVENT \
			-u GIT_TRACE2_PERF \
			-u GIT_TRACE2_BRIEF \
			GIT_CONFIG_GLOBAL=/dev/null \
			GIT_CONFIG_SYSTEM=/dev/null \
			GIT_CONFIG_NOSYSTEM=1 \
			GIT_ATTR_NOSYSTEM=1 \
			GIT_OPTIONAL_LOCKS=0 \
			git "$$@"; }; \
		repository="$$(git_ro rev-parse --show-toplevel 2>/dev/null)" || { printf '%s\n' 'aquarium-dev: build must run at a Git repository root' >&2; exit 1; }; \
		if [[ "$$(pwd -P)" != "$$repository" ]]; then \
			printf '%s\n' 'aquarium-dev: build must run at the Git repository root' >&2; \
			exit 1; \
		fi; \
		branch="$$(git_ro symbolic-ref --quiet --short HEAD 2>/dev/null)" || { printf '%s\n' 'aquarium-dev: build requires local main, not detached HEAD' >&2; exit 1; }; \
		if [[ "$$branch" != main ]]; then \
			printf '%s\n' 'aquarium-dev: build requires the local main branch' >&2; \
			exit 1; \
		fi; \
		git_sha="$$(git_ro rev-parse HEAD 2>/dev/null)" || { printf '%s\n' 'aquarium-dev: cannot resolve HEAD' >&2; exit 1; }; \
		if ! printf '%s\n' "$$git_sha" | grep -Eq '^[0-9a-f]{40}$$'; then \
			printf '%s\n' 'aquarium-dev: admitted Git SHA must be 40 lowercase hexadecimal characters' >&2; \
			exit 1; \
		fi; \
		main_sha="$$(git_ro rev-parse --verify refs/heads/main 2>/dev/null)" || { printf '%s\n' 'aquarium-dev: local main ref is unavailable' >&2; exit 1; }; \
		if [[ "$$git_sha" != "$$main_sha" ]]; then \
			printf '%s\n' 'aquarium-dev: HEAD must equal refs/heads/main' >&2; \
			exit 1; \
		fi; \
		if ! git_status="$$(git_ro status --porcelain=v1 --untracked-files=all 2>/dev/null)"; then \
			printf '%s\n' 'aquarium-dev: cannot inspect Git checkout status' >&2; \
			exit 1; \
		fi; \
		if [[ -n "$$git_status" ]]; then \
			printf '%s\n' 'aquarium-dev: build requires a clean checkout' >&2; \
			exit 1; \
		fi; \
		source_dir="$$output/.source"; \
		cache_root="$$output/.cache"; \
		cache_dir="$$cache_root/go-build"; \
		module_cache_dir="$$cache_root/go-mod"; \
		tmp_dir="$$output/.tmp"; \
		archive_file="$$tmp_dir/source.tar"; \
		git_mirror="$$tmp_dir/repository.git"; \
		export TMPDIR="$$tmp_dir"; \
		cleanup() { status=$$?; chmod -R u+w "$$source_dir" "$$cache_root" "$$tmp_dir" "$$output/.home" 2>/dev/null || true; rm -rf -- "$$source_dir" "$$cache_root" "$$tmp_dir" "$$output/.home" 2>/dev/null || true; if [[ "$$status" -ne 0 ]]; then rm -f -- "$$output/bin/sanho" 2>/dev/null || true; rmdir "$$output/bin" 2>/dev/null || true; fi; return "$$status"; }; \
		trap cleanup EXIT; \
		mkdir -p "$$source_dir" "$$cache_dir" "$$module_cache_dir" "$$tmp_dir"; \
		git_ro clone --quiet --no-hardlinks --bare -- "$$repository" "$$git_mirror"; \
		git_ro -C "$$git_mirror" -c core.attributesfile=/dev/null archive --format=tar "$$git_sha" > "$$archive_file"; \
		tar -xf "$$archive_file" -C "$$source_dir"; \
		source_version="$$(awk -F'"' '/^[[:space:]]*CurrentVersion[[:space:]]*=[[:space:]]*"/ { print $$2; exit }' "$$source_dir/internal/buildinfo/version.go")"; \
		if ! printf '%s\n' "$$source_version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$$'; then \
			printf '%s\n' 'aquarium-dev: committed CurrentVersion is invalid' >&2; \
			exit 1; \
		fi; \
		development_version="$$source_version-dev.$${git_sha:0:12}"; \
		artifact="$$output/bin/sanho"; \
		mkdir -p "$$output/bin"; \
		( cd -- "$$source_dir" && env -i \
			PATH="$$PATH" \
			HOME="$$output/.home" \
			TMPDIR="$$tmp_dir" \
			GOCACHE="$$cache_dir" \
			GOMODCACHE="$$module_cache_dir" \
			GOPATH="$$cache_root/gopath" \
			GOTMPDIR="$$tmp_dir" \
			GOENV=off \
			GOWORK=off \
			GOFLAGS= \
			GOMOD= \
			GOTOOLCHAIN=local \
			GO111MODULE=on \
			CGO_ENABLED=0 \
			GOOS=darwin \
			GOARCH=arm64 \
			go build -buildvcs=false -trimpath -ldflags "-X main.version=$$development_version -X main.gitSHA=$$git_sha" -o "$$artifact" ./cmd/sanho ); \
		checksum="$$(shasum -a 256 "$$artifact" | awk '{print $$1}')"; \
		if ! printf '%s\n' "$$checksum" | grep -Eq '^[0-9a-f]{64}$$'; then \
			printf '%s\n' 'aquarium-dev: could not calculate the executable checksum' >&2; \
			exit 1; \
		fi; \
		printf '{"schema":"aquarium-dev-artifact-manifest/v1","project_id":"sanho","git_sha":"%s","development_version":"%s","artifact_kind":"executable","artifact_path":"bin/sanho","sha256":"sha256:%s"}\n' "$$git_sha" "$$development_version" "$$checksum"

docs-check:
	@test -f README.md
	@test -f CHANGELOG.md
	@test -f docs/README.md
	@test -f docs/architecture.md
	@test -f docs/architecture/README.md
	@test -f docs/architecture-decision-records/README.md
	@test -f docs/cli-json.md
	@test -f docs/deferred-feedback/README.md
	@test -f docs/deployment.md
	@test -f docs/hands-on-testing.md
	@test -f docs/implementation-tips/README.md
	@test -f docs/implementation-tips/agent-skill-verification.md
	@test -f docs/ops/README.md
	@test -f docs/operations.md
	@test -f docs/recovery.md
	@test -f docs/roadmap/README.md
	@test -f docs/specs/README.md
	@test -f docs/todo/README.md
	@test -f skills/use-sanho/SKILL.md
	@test -f skills/use-sanho/references/lifecycle.md
	@test -f skills/use-sanho/references/authoring.md
	@test -f skills/use-sanho/references/recovery.md
	@test -f skills/use-sanho/references/inspection.md
	@if grep -REn '[가-힣]' README.md docs skills; then \
		echo "Error: repository documentation must be English-only."; \
		exit 1; \
	fi
	@if grep -REn 'sanho-v0\.2\.md' AGENTS.md README.md docs skills cmd internal test; then \
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
	@if grep -REn 'docs/requirement\.md|build-server-with-web|run-web-local|run-local-dev-with-web|WEB_DIST_DIR|PTY_' README.md docs skills; then \
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
	$(GO) test ./test/aquariumdev -count=1 -v

# The native Aquarium consumer is an explicit, opt-in integration boundary.
# Portable repository checks skip its Go test when the external checkout is
# absent; this target requires both the checkout and its reviewed revision so
# a requested real-consumer run cannot pass without executing the consumer.
# The Go timeout covers five possible 600-second native rebuilds plus probes,
# the direct producer probes, and cleanup; keep the aggregate envelope at 70m.
aquarium-dev-realconsumer:
	@if [[ "$$(uname -s)" != Darwin || "$$(uname -m)" != arm64 ]]; then \
		echo 'Error: the Aquarium consumer check requires Darwin arm64.' >&2; \
		exit 2; \
	fi
	@goos="$$( $(GO) env GOOS )"; \
		goarch="$$( $(GO) env GOARCH )"; \
		if [[ "$$goos $$goarch" != 'darwin arm64' ]]; then \
			echo "Error: the Aquarium consumer check requires a Darwin arm64 Go target (got $${goos:-unknown}/$${goarch:-unknown})." >&2; \
			exit 2; \
		fi
	@if [[ -z "$${SANHO_AQUARIUM_ROOT:-}" || "$${SANHO_AQUARIUM_ROOT:0:1}" != / ]]; then \
		echo 'Error: SANHO_AQUARIUM_ROOT must name the absolute Aquarium checkout.' >&2; \
		exit 2; \
	fi
	@if [[ -z "$${SANHO_AQUARIUM_REVISION:-}" || ! "$${SANHO_AQUARIUM_REVISION}" =~ ^[0-9a-f]{40}$$ ]]; then \
		echo 'Error: SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA.' >&2; \
		exit 2; \
	fi
	SANHO_REALCONSUMER=1 $(GO) test ./test/aquariumdev -run '^(TestRealConsumer|TestConsumerEnvironmentStripsInheritedGit|TestTimedMakePreservesGitIsolation)$$' -count=1 -v -timeout 70m

# test/cli/e2e is the v0.2 scenario suite restored by P5: the guidance
# guidance-closure table, the scenario matrix, and process-level concurrency.
#
# It runs WITHOUT -race, deliberately. Every assertion here is about
# separate `sanho` and `git` *processes*, so the detector would only
# instrument the test harness that spawns them — buying nothing while
# roughly halving throughput. The in-process suites carry -race: test-unit,
# test/cli/integration, and test/docsync. The test/aquariumdev producer suite
# also drives make, Git, and Go as child processes, so it follows this
# process-level boundary.
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
