# Repository Guidelines

## Project Structure & Module Organization
- `cmd/sanhod` hosts the sanhod HTTP service; `cmd/sanho` is the CLI entrypoint.
- Core logic sits in `internal/{config,domain,infra,interface,usecase}`; keep new packages domain-oriented.
- Docs/roadmaps live in `docs/`; fixture docs repos for tests in `docs_repos/`; runtime artifacts in `data/` and `tmp/` (ignored); builds in `bin/`.
- Tests: co-locate package unit tests as `*_test.go`; server integration in `test/integration`, server e2e in `test/e2e`; CLI integration/e2e in `test/cli/...`.

## Language Policy
- Code, inline comments, and all CLI/HTTP interfaces stay in English.
- Documentation under `docs/` and team communication (including this guide and conversations) should be written in Korean.

## Build, Test, and Development Commands
- Require Go 1.25+. Local daemon: `go run ./cmd/sanhod` (override `SANHO_HOME`, `SANHO_SOCKET`).
- Build/install: `make daemon-build` → `bin/sanhod`, `make cli-build` → `bin/sanho`; `make install` installs both to Go's binary directory.
- Server checks: `make daemon-test-prepare` (generate+fmt+vet), `make daemon-test-unit`, `make daemon-test-integration`, `make daemon-test-e2e` (isolated daemon by default; set `E2E_SOCKET` for an explicit server), or `make daemon-test` for all.
- CLI checks: `make cli-test-prepare`, `make cli-test-unit`, `make cli-test-integration` (sets `SANHO_CLI_BINARY`), `make cli-test-e2e` (`SANHO_E2E_SOCKET`), or `make cli-test`.
- Local daemon loop: `make daemon-run` builds and runs `bin/sanhod`.

## Coding Style & Naming Conventions
- Use standard Go formatting (`go fmt ./...` is in prep targets); exported names follow Go casing, packages stay lowercase.
- Keep names explicit about intent and side effects; command wiring in `cmd/sanho`, HTTP handlers in `internal/interface/http`, domain types in `internal/domain`.
- Tests use `TestXxx`/`BenchmarkXxx` patterns; table tests for branch-heavy logic are preferred.

## Testing Guidelines
- Add unit tests near new code; move cross-adapter cases to `test/integration` and end-to-end flows to `test/e2e` or `test/cli/e2e`.
- Set `SANHO_E2E_SOCKET` for non-default servers; point `SANHO_CLI_BINARY` at a fresh build for CLI suites.
- Keep failing tests that capture expected behavior when fixing regressions; aim for coverage on new branches.

## Commit & Pull Request Guidelines
- Commit style matches history: `[TYPE] Brief summary (#issue-or-PR)` (e.g., `[BUG-3] Fix pending fix merge edge case (#42)`); one logical change per commit.
- PRs outline scope, validation steps, config/env changes, linked issues; include screenshots only when output matters.
- Call out deferred follow-ups explicitly so they can be tracked.

## Security & Configuration Tips
- Do not commit secrets; `.sanho*`, `data/`, and temp repos should stay untracked (init updates `.gitignore`).
- Prefer disposable repos under `/tmp` for e2e runs to avoid polluting real workspaces.
