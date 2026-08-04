# AGENTS.md

Repository guidance for AI coding agents working on Sanho.

This file is the complete local authority for how agents inspect, implement,
and verify work in this repository. Apply these rules proportionally: preserve
the same safety boundaries for every task without adding ceremony that does not
improve a small change.

## Core Behavior

### 1. Inspect Before Acting

**Resolve repository facts before making implementation decisions. Do not hide uncertainty.**

Before implementing:

- Read the requested code and the nearest relevant authority before changing anything.
- Inspect the worktree, index, and relevant runtime or Git state when they can affect the task.
- Resolve discoverable facts from the repository instead of asking the user for information the repository already provides.
- State assumptions that materially affect scope, design, compatibility, safety, or verification.
- If multiple interpretations would produce materially different outcomes, present the alternatives and recommend one instead of choosing silently.
- Surface meaningful trade-offs and prefer a simpler approach when it satisfies the same requirement with less complexity or risk.
- Stop and ask a focused question when unresolved ambiguity would materially change the result.
- Push back when a request conflicts with repository authority, safety boundaries, or the user's stated goal.

### 2. Prefer the Smallest Complete Solution

**Use the minimum implementation that fully satisfies the verified requirement. Add nothing speculative.**

- Implement only what the request requires.
- Reuse established repository patterns before introducing a new abstraction.
- Do not add speculative features, configurability, compatibility layers, or extension points.
- Add defensive handling at real trust, persistence, concurrency, process, network, filesystem, and Git boundaries, not for states repository invariants make impossible.
- Prefer a robust implementation when the requirement warrants it, but remove layers justified only by possible future needs.
- If the implementation is substantially larger than the behavior it provides, simplify it before reporting completion.

### 3. Make Surgical Changes

**Touch only what the requested outcome and its verification require. Clean up only what the change makes obsolete.**

- Do not refactor, reformat, rename, or clean up adjacent code unless the task requires it.
- Match the local style even when another style would also work.
- Preserve unrelated staged, unstaged, and untracked user changes.
- Mention unrelated defects or dead code instead of modifying them without authorization.
- Remove imports, variables, functions, files, generated references, or documentation made obsolete by the requested change.
- Do not remove pre-existing dead code or unrelated artifacts unless the request includes that cleanup.

Every changed line must be traceable to the requested outcome or its verification.

### 4. Work Toward Verifiable Goals

**Define success before implementation and continue until the result is proved or concretely blocked.**

- Translate the request into explicit success checks before implementing.
- For a bug, reproduce the failure when practical and add or identify a regression check that fails for the right reason before making it pass.
- For a behavior change, add or update tests that prove the requested contract and relevant failure paths.
- For a refactor, establish the relevant behavior and checks before editing, then run them again afterward.
- Run focused checks first, then broader repository-standard checks when their cost and scope are justified.
- Use `Makefile` targets for repository-standard generation, lint, build, and test workflows instead of inventing parallel commands.
- Do not treat scaffolding, compilation alone, mocked success, or partial checks as proof when acceptance requires stronger evidence.
- Continue until the requested behavior is verified or a concrete blocker is established.
- Report skipped checks with the reason and distinguish assumptions from confirmed results.

## Repository Authorities

- Use `README.md` for the product boundary, supported components, public workflows, and top-level validation entrypoint.
- Use `docs/architecture.md` for runtime, Git, synchronization, persistence, concurrency, and safety contracts.
- Use `docs/operations.md`, `docs/deployment.md`, `docs/cli-json.md`, and `docs/hands-on-testing.md` for their respective operational, deployment, interface, and real-environment verification details.
- Use `CHANGELOG.md` for released behavior and compatibility history, not as authority for unimplemented future work.
- Use `Makefile` as the entry point for repository-standard generation, lint, build, and test commands.
- Read the nearest relevant implementation and tests instead of copying detailed feature behavior into this file.
- If authorities or implementation disagree, surface the mismatch and resolve it before changing behavior; do not silently choose the convenient source.

## Project Structure & Module Organization
- `cmd/sanhod` hosts the sanhod HTTP service; `cmd/sanho` is the CLI entrypoint.
- Core logic sits in `internal/{config,domain,infra,interface,usecase}`; keep new packages domain-oriented.
- Docs/roadmaps live in `docs/`; fixture docs repos for tests in `docs_repos/`; runtime artifacts in `data/` and `tmp/` (ignored); builds in `bin/`.
- Tests: co-locate package unit tests as `*_test.go`; daemon integration in `test/integration`, daemon e2e in `test/e2e`; CLI integration/e2e in `test/cli/...`.

## Language Policy
- Code, inline comments, and all CLI/HTTP interfaces stay in English.
- Documentation under `docs/` and team communication, including conversations, should be written in Korean. Keep this repository guidance file in English.

## Project-Specific Operating Rules

- Do not commit, push, release, install binaries, operate a real remote, or start, stop, replace, or reconfigure an active daemon without explicit authorization.
- Do not discard, overwrite, unstage, or otherwise disturb unrelated user changes or Git operation metadata.
- Never bypass Sanho or Git safety guards with `--no-verify`, force operations, manual metadata deletion, or direct mutation of Sanho-managed state.
- Use checkout-built `bin/sanho` and `bin/sanhod` with isolated `SANHO_HOME` and socket paths when validation must prove current source behavior.
- Prefer disposable Git repositories under `/tmp` for integration, E2E, hook, and real-remote fixtures. Do not point tests at production-like repositories or an active `xyz.rootkernel.sanho` service unless the user explicitly selects them.
- Never edit generated code manually. Change its source and use the documented generator or `Makefile` target.
- Keep completion reports compact: state the outcome, changed files, verification performed, and actionable remaining risks or blockers.

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
