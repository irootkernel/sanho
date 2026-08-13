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
- Use `docs/architecture.md` for runtime, Git, provenance, publication, synchronization, persistence, concurrency, and safety contracts. It is the implementation authority.
- Use `docs/operations.md`, `docs/recovery.md`, `docs/deployment.md`, `docs/cli-json.md`, and `docs/hands-on-testing.md` for their respective operational, recovery, deployment, interface, and real-environment verification details.
- Use `CHANGELOG.md` and Git history for released behavior and compatibility history, not as authority for current implementation or unimplemented future work.
- Use `Makefile` as the entry point for repository-standard generation, lint, build, and test commands.
- Read the nearest relevant implementation and tests instead of copying detailed feature behavior into this file.
- If authorities or implementation disagree, surface the mismatch and resolve it before changing behavior; do not silently choose the convenient source.

## Project Structure & Module Organization
- `cmd/sanho` is the only entrypoint. Sanho ships a single binary and has no daemon.
- Core logic sits in `internal/{buildinfo,domain,infra,interface,usecase}`; keep new packages domain-oriented.
- Layering is enforced by `internal/architecture`: a `usecase` package must not import `infra`, and an `infra` package must not import `usecase`. `internal/interface/cli` is the only place that sees both, so it is where adapters are bound to declared ports.
- Docs live in `docs/`; optional source-distributed AI-agent guidance lives in `skills/`; runtime artifacts live in `data/` and `tmp/` (ignored); builds live in `bin/`.
- Tests: co-locate package unit tests as `*_test.go`; black-box CLI behavior in `test/cli/integration`; guidance-closure and scenario suites in `test/cli/e2e`; sync/pull flow coverage in `test/docsync`; the install check in `test/install`.

## Language Policy
- Code, inline comments, and all CLI/HTTP interfaces stay in English.
- Documentation under `docs/` and `skills/`, the root README, code, inline comments, and all CLI/HTTP interfaces must be written in English. Team communication may use the user's language. Keep this repository guidance file in English.

## Project-Specific Operating Rules

- Do not commit, push, release, install binaries, or operate a real remote without explicit authorization.
- Do not discard, overwrite, unstage, or otherwise disturb unrelated user changes or Git operation metadata.
- Never bypass Sanho or Git safety guards with `--no-verify`, force operations, manual metadata deletion, or direct mutation of Sanho-managed state.
- Use the checkout-built `bin/sanho` with an isolated `SANHO_HOME` when validation must prove current source behavior.
- Prefer disposable Git repositories under a temporary directory for integration, hook, and real-remote fixtures. Do not point tests at production-like repositories unless the user explicitly selects them.
- Never edit generated code manually. Change its source and use the documented generator or `Makefile` target.
- Keep completion reports compact: state the outcome, changed files, verification performed, and actionable remaining risks or blockers.

## Build, Test, and Development Commands
- Require Go 1.25+ to build. Git is required at runtime; no minimum git version is enforced, though merge paths need git 2.38+ in practice.
- Build/install: `make cli-build` → `bin/sanho`; `make cli-install` (aliased by `make install`) installs it to Go's binary directory. `build-cli` and `install-cli` remain as compatibility aliases.
- The complete `make test` verification runs `test-prepare`, `test-unit`, `test-int`, and `test-e2e` sequentially.
- `test-prepare` runs generation, formatting, module verification, `docs-check`, `test-package-ownership`, `test-architecture`, vet, and lint. `test-unit` runs the unit packages with `-race`. `test-int` builds `bin/sanho`, passes it through `SANHO_CLI_BINARY`, and runs `test/cli/integration` and `test/docsync`. `test-e2e` drives the built binary through `test/cli/e2e` (the scenario matrix, process-level concurrency, and the guidance-closure suite) and runs the `go install` check in `test/install`.
- Adding a package under `cmd/...` or `internal/...` requires adding it to `UNIT_PACKAGES` in the `Makefile`, or `test-package-ownership` fails.
- `make docs-check` asserts that the documented file set exists and greps for retired references; keep it green.

## Coding Style & Naming Conventions
- Use standard Go formatting (`go fmt ./...` is in prep targets); exported names follow Go casing, packages stay lowercase.
- Keep names explicit about intent and side effects; command and hook wiring in `internal/interface/cli`, flow orchestration in `internal/usecase`, pure decisions in `internal/domain`, git execution in `internal/infra/gitx`.
- Every user-facing string that names a next command belongs in `internal/interface/cli/messages.go` and in that file's guidance catalog, not at its call site. A unit test parses the file as source and fails the build when a message advises a command without a catalog entry, and the `test/cli/e2e` closure suite then runs that command in the state the message is printed in.
- Tests use `TestXxx`/`BenchmarkXxx` patterns; table tests for branch-heavy logic are preferred.

## Testing Guidelines
- Add unit tests near new code; move cross-adapter flow cases to `test/docsync` and black-box command behavior to `test/cli/integration`.
- Do not mock below the git boundary. Merge, publication, base re-derivation, and marker-detection logic are tested against real `git` in temporary repositories.
- Point `SANHO_CLI_BINARY` at a fresh build for the CLI suites; `make test-int` does this. Use an isolated `SANHO_HOME` in anything that touches the registry.
- Keep failing tests that capture expected behavior when fixing regressions; aim for coverage on new branches.

## Gaori Test Evidence
- The repository documentation and task scope remain authoritative for deciding which tests are required. Gaori is an optional execution and evidence-compression adapter, not an additional test gate or acceptance authority.
- Run tests that are expected to be long or noisy through Gaori from the repository root: preparation `gaori --json run prepare`, unit `gaori --json run unit`, integration `gaori --json run integration`, E2E `gaori --json run e2e`, and the complete suite `gaori --json run all`.
- Run a dynamically selected Go test as `gaori --json run --parser go-test --tag go --tag unit -- go test <package> <test arguments>`. Narrower Make subtasks may use the same ad-hoc form with a parser and phase tags that match their actual output.
- Before the first Gaori run in a task, record `gaori --version`. Configured commands require the local `.gaori/tester.yaml`. Use the installed Gaori version rather than requiring a repository-pinned version. If the binary or config is unavailable, use the repository's normal documented test command and report that Gaori evidence compression was unavailable. Do not install or upgrade Gaori or change local Gaori state without an explicit user request.
- The executed command's exit code is authoritative for pass/fail. `extractor_status` describes evidence quality only and never changes the result. Tags select project rules, not parsers, and specialized parsers do not automatically fall back to `generic`.
- When a command passes, do not open its generated logs by default. When it does not pass, inspect `*.summary.md` first, followed by `*.summary.json` or a bounded excerpt for the relevant failure. Read only a bounded raw-log section when compact evidence is insufficient or degraded. Open or share `*.raw.log` only when necessary because raw logs are preserved without redaction and may contain secrets.
- Keep the entire `.gaori/` directory out of Git. Do not add or commit its config, rules, toolchain metadata, proposals, or evidence.
- In the final report, include the Gaori command, process exit code, artifact `status`, `extractor_status`, relevant summary and raw-log paths, and any skipped checks. Gaori evidence alone does not establish review acceptance, final acceptance, release, or runtime activation.

## Mulgae Code Review
- Use Mulgae only when the user explicitly asks for a Mulgae review. Mulgae is advisory and does not replace repository requirements, `make test`, Gaori evidence, hands-on validation, or user approval.
- Before a review, verify that `mulgae version --json` succeeds and that `.mulgae/config.yaml` exists. If either prerequisite is missing, stop and report it. Do not run `mulgae init` unless the user separately and explicitly asks for initialization.
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
- Prefer disposable repos under a temporary directory for end-to-end runs to avoid polluting real workspaces.
- Preserve the existing permission discipline: the sanho home is `0700`, the registry and its backup are `0600`, and every state write goes through the shared atomic writer in `internal/infra/fsx`.
- Keep git invocations argv-only through `internal/infra/gitx`. Never build a shell command line, and never drop `GIT_TERMINAL_PROMPT=0` or the network runner's SSH `BatchMode` policy.
