# Repository Guidelines

## Project Structure & Module Organization
- `cmd/sanhod` hosts the sanhod HTTP service; `cmd/sanho` is the CLI entrypoint.
- Core logic sits in `internal/{config,domain,infra,interface,usecase}`; keep new packages domain-oriented.
- Docs/roadmaps live in `docs/`; fixture docs repos for tests in `docs_repos/`; runtime artifacts in `data/` and `tmp/` (ignored); builds in `bin/`.
- Tests: co-locate package unit tests as `*_test.go`; daemon integration in `test/integration`, daemon e2e in `test/e2e`; CLI integration/e2e in `test/cli/...`.

## Language Policy
- Code, inline comments, and all CLI/HTTP interfaces stay in English.
- Documentation under `docs/` and team communication, including conversations, should be written in Korean. Keep this repository guidance file in English.

## Build, Test, and Development Commands
- Require Go 1.25+. Local daemon: `go run ./cmd/sanhod` (override `SANHO_HOME`, `SANHO_SOCKET`).
- Build/install: `make daemon-build` → `bin/sanhod`, `make cli-build` → `bin/sanho`; `make install` installs both to Go's binary directory.
- The complete `make test` verification runs `test-prepare`, `test-unit`, `test-int`, and `test-e2e` sequentially.
- Each phase can be narrowed to its `-daemon` or `-client` target. E2E uses an isolated daemon by default; set `E2E_SOCKET` only when targeting an explicitly selected daemon.
- Local daemon loop: `make daemon-run` builds and runs `bin/sanhod`.

## Coding Style & Naming Conventions
- Use standard Go formatting (`go fmt ./...` is in prep targets); exported names follow Go casing, packages stay lowercase.
- Keep names explicit about intent and side effects; command wiring in `cmd/sanho`, HTTP handlers in `internal/interface/http`, domain types in `internal/domain`.
- Tests use `TestXxx`/`BenchmarkXxx` patterns; table tests for branch-heavy logic are preferred.

## Testing Guidelines
- Add unit tests near new code; move cross-adapter cases to `test/integration` and end-to-end flows to `test/e2e` or `test/cli/e2e`.
- Set `SANHO_E2E_SOCKET` for non-default daemons; point `SANHO_CLI_BINARY` at a fresh build for CLI suites.
- Keep failing tests that capture expected behavior when fixing regressions; aim for coverage on new branches.

## Gaori Test Evidence
- The repository documentation and task scope remain authoritative for deciding which tests are required. Gaori is an optional execution and evidence-compression adapter, not an additional test gate or acceptance authority.
- Run tests that are expected to be long or noisy through Gaori from the repository root: preparation `gaori --json run prepare`, unit `gaori --json run unit`, integration `gaori --json run integration`, E2E `gaori --json run e2e`, and the complete suite `gaori --json run all`.
- Run a dynamically selected Go test as `gaori --json run --parser go-test --tag go --tag unit -- go test <package> <test arguments>`. Daemon/client Make subtasks may use the same ad-hoc form with a parser and phase tags that match their actual output.
- Before the first Gaori run in a task, verify that `gaori --version` reports exactly `gaori v0.1.8`. Configured commands require the local `.gaori/tester.yaml`. If the binary, expected version, or config is unavailable, use the repository's normal documented test command and report that Gaori evidence compression was unavailable. Do not install or upgrade Gaori or change local Gaori state without an explicit user request.
- The executed command's exit code is authoritative for pass/fail. `extractor_status` describes evidence quality only and never changes the result. Tags select project rules, not parsers, and specialized parsers do not automatically fall back to `generic`.
- When a command passes, do not open its generated logs by default. When it does not pass, inspect `*.summary.md` first, followed by `*.summary.json` or a bounded excerpt for the relevant failure. Read only a bounded raw-log section when compact evidence is insufficient or degraded. Open or share `*.raw.log` only when necessary because raw logs are preserved without redaction and may contain secrets.
- Keep the entire `.gaori/` directory out of Git. Do not add or commit its config, rules, toolchain metadata, proposals, or evidence.
- In the final report, include the Gaori command, process exit code, artifact `status`, `extractor_status`, relevant summary and raw-log paths, and any skipped checks. Gaori evidence alone does not establish review acceptance, final acceptance, release, or runtime activation.

## Mulgae Code Review
- Use Mulgae only when the user explicitly asks for a Mulgae review. Mulgae is advisory and does not replace repository requirements, `make test`, Gaori evidence, hands-on validation, or user approval.
- Before a review, verify that `mulgae version --json` reports exactly `{"name":"mulgae","version":"v0.1.2"}` and that `.mulgae/config.yaml` exists. If either prerequisite is missing, stop and report it. Do not run `mulgae init` unless the user separately and explicitly asks for initialization.
- Select exactly one target matching the requested scope: `--diff <base>...HEAD` for a branch or pull request, `--stage` for staged changes, `--dirty` for staged and unstaged changes, and `--workspace` only when the user explicitly requests all tracked files at the current workspace state.
- Use all six configured roles by default: `--roles logic,security,maintainability,product,documentation,testing`. Use a subset only when the user explicitly narrows the review roles.
- Write an objective that states the task goal, authoritative requirements, relevant invariants, expected failure boundaries, and desired validation focus. The objective may narrow focus but must not override a role, schema, safety rule, evidence rule, or authority boundary.
- Before invoking providers, run the same review command with `--preflight --output json`. Inspect the exact transmitted files, primary and fallback routes, AGY permission mode, provider timeouts, and run budget. Stop if the transmission contains an unexpected or sensitive path or the execution envelope differs from the configured policy.
- Run the approved review with `--output json`. Exit status 0 is success and status 1 is a policy outcome; any other status is an operational failure and must be reported without bypassing Mulgae.
- Preserve the exact run ID. Inspect results with `mulgae status --run <run-id> --output json` and `mulgae findings --run <run-id> --severity low --output json`.
- Treat every finding as a hypothesis. Verify it against the captured target, current code, repository contracts, and authorized scope; classify it as valid, invalid, duplicate, or out of scope before proposing action. Do not edit, commit, push, release, or operate a daemon based only on a Mulgae finding.
- After an authorized fix, use the original run ID and finding ID in `mulgae followup` with a target containing the fix. Do not treat evidence verification as proof that the finding's interpretation is correct.
- Do not commit or share `.mulgae/`, provider credential directories, raw transcripts, or exported review bundles. Report Mulgae execution and skipped repository checks separately in the final handoff.

## Commit & Pull Request Guidelines
- Commit style matches history: `[TYPE] Brief summary (#issue-or-PR)` (e.g., `[BUG-3] Fix pending fix merge edge case (#42)`); one logical change per commit.
- PRs outline scope, validation steps, config/env changes, linked issues; include screenshots only when output matters.
- Call out deferred follow-ups explicitly so they can be tracked.

## Security & Configuration Tips
- Do not commit secrets; `.sanho*`, `data/`, and temp repos should stay untracked (init updates `.gitignore`).
- Prefer disposable repos under `/tmp` for e2e runs to avoid polluting real workspaces.
