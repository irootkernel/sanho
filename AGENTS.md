# AGENTS.md

Repository guidance for AI coding agents working on Sanho.

This file is the complete local authority for how agents inspect, implement,
and verify work in this repository. Apply these rules proportionally: preserve
the same safety boundaries for every task without adding ceremony that does not
improve a small change.

## Core Behavior

### 1. Lead with Conclusions

- State the result or current finding first, followed by useful evidence and material limits.
- Do not repeatedly restate requirements or narrate routine work.

### 2. Reuse Verified Information

- Inspect the requested code and its named authorities before changing anything. Resolve discoverable facts before asking Master.
- Inspect the worktree, index, and relevant runtime or Git state when they can affect the task.
- When materially different interpretations remain, present the alternatives and recommend one before asking a focused question.
- Reuse established facts instead of reading or searching for them again. Recheck only the affected information when relevant state changes, evidence conflicts, or missing context makes it unreliable.
- State material assumptions and surface meaningful trade-offs. Ask when unresolved ambiguity would materially change the result, and push back on conflicts with repository authority, safety, or Master's goal.

### 3. Act on Sufficient Evidence

- Stop investigating once the evidence supports action. When the root cause is established, implement the smallest complete, durable fix within the authorized scope.
- Weigh correctness, performance, maintainability, and structural fit rather than diff size alone. If a broader design exceeds scope, complete a bounded step that satisfies current acceptance criteria.
- Reuse established patterns. Avoid speculative features, abstractions, configurability, compatibility layers, and handling for states repository invariants make impossible. Simplify complexity that the required behavior does not justify.
- Add defensive handling at real trust, persistence, concurrency, process, network, filesystem, and Git boundaries.
- Touch only what the outcome and its verification require. Preserve unrelated user work, match local style, and remove only artifacts made obsolete by this change.
- Do not refactor, reformat, rename, or clean up adjacent code unless required. Mention unrelated defects or pre-existing dead code without modifying them. Every changed line must serve the requested outcome or its verification.
- Record only independent remaining work in the canonical `deferred-feedback` owner. If none exists, propose the entry and obtain approval before creating an owner. Promote epic-sized work to a TODO candidate or roadmap unit; never defer current correctness or acceptance work.

### 4. Carry Authorization Forward

- Continue already approved work without asking for confirmation again. Ask only when a material change exceeds that authorization or an applicable rule requires a distinct approval.
- Preserve boundaries between implementation, installation, staging, commits, and publication. Check for relevant state changes before acting on an approved proposal.

### 5. Verify in Proportion to Risk

- Define success checks before implementation. Verify the affected behavior and relevant failure paths with rigor proportionate to the actual risk.
- For bugs, reproduce the failure when practical and retain a regression check that fails for the right reason. For behavior changes, add or update contract and failure-path tests. For refactors, establish relevant checks before editing and rerun them afterward.
- Do not treat scaffolding, compilation alone, mocked success, or partial checks as proof when acceptance requires stronger evidence.
- Run focused checks first and honor required repository gates. Broaden or repeat checks when changes, failures, or unresolved concerns justify it.
- Do not add tests merely to appear rigorous or use prose matching as a substitute for behavior verification.

### 6. Finish When Complete

- Continue until deliverables and required verification are complete or a concrete blocker prevents progress.
- Once material constraints are resolved or clearly reported, provide the handoff and stop. Report the result, necessary evidence, skipped checks and their reasons, and remaining uncertainty without opening unrelated work.

### 7. Delegate Selectively

- Use a sub-agent only for an independent task when the expected benefit outweighs coordination cost.
- Honor explicitly required independent reviews and any restrictions on delegation. Keep tightly coupled work local.

## Master Preferences

- Respond to Master in Korean using polite speech. When directly addressing the user, use exactly `Master`.
- Keep repository artifacts in the repository's established language and style. When no convention exists, use English unless Master requests otherwise.
- Report concise conclusions and useful evidence without exposing private chain-of-thought.

## Aquarium Development Guide

- Use `$aquarium:task-handler` for one named roadmap task.
- Use `$aquarium:epic-handler` to implement one roadmap epic as sequential task goals.
- Use `$aquarium:epic-validator` to cold-validate and remediate one completed roadmap epic.
- Use `$aquarium:new-project`, `$aquarium:new-feature`, or `$aquarium:refactor` for an explicitly requested Ouroboros-assisted project or epic design workflow.
- Use `$aquarium:war-room` to diagnose one difficult bug and stop at a task, epic, or incomplete-investigation proposal.
- Use `$aquarium:dev-setup-global` to diagnose, install, or update user-global development tools, paired skills, services, and global MCP state.
- Use `$aquarium:dev-setup` to diagnose or configure repository-local tooling and operating guidance.
- Use `$aquarium:docs-setup` to audit, establish, adopt, or migrate canonical documentation structure and roadmap IDs.
- Use `$aquarium:test-setup` to audit or configure the common Make testing contract and evidence-backed legacy waivers.
- Use `$aquarium:release-handler` for one stable release lifecycle and `$aquarium:release-qa` for its exact committed-candidate scenario verification.
- Use `$use-dolgorae` for explicitly requested workspace, global Profile, review, External Specialist Engagement, and recovery operations. Keep execution and lifecycle rules in the paired skill.
- Use `$use-sorage` only when Master explicitly requests Sorage operations. Do not invoke it automatically at session start or before tasks, even if the paired skill says otherwise. Resolve authorized inbox, outbox, Handoff, review, retention, deletion, and Vault operations through that skill; never edit the managed Vault or derived `.sorage/INBOX.md` directly.
- Use `$use-sanho` at an authorized commit or push boundary in a Sanho-managed repository, or for an explicitly requested Sanho operation.
- Use `$use-mulgae` as the native authority for authorized asynchronous reviews, waiting, cancellation, evidence inspection, configuration diagnosis, cleanup plans, and recovery. Aquarium workflows own the target, approval criteria, and review-round accounting.
- Use `$use-gaori` as the native authority for asynchronous execution, waiting, cancellation, and recovery when a selected check uses Gaori or existing evidence must be inspected. Repository requirements select the command; Aquarium evaluates the terminal result and evidence.
- Use `$use-gaori-status` for Gaori-calculated duration, outcome history, and detailed timing explanations; its absence does not block a selected check.
- Let Git-backed Aquarium workflows use Podway by default unless the current user opts out before the first managed-session mutation. No Aquarium skill owns a Podway session; only when starting a different session should the workflow ask whether to preserve, finish, delete, or replace the existing one.
- Use `$use-podway` directly for an explicitly requested Procedure v2 lifecycle, goal, diagnosis, recovery, cancellation, or discard operation. Route Procedure authoring to `$create-podway-procedure`. Keep each handler opt-out local to its current task, epic, or validation request.
- Use `$lore-commits` for non-trivial commit messages and `$lore-query` to inspect recorded decision context.
- Use the separately installed upstream `$deslop` skill for task-owned cleanup when an Aquarium workflow requests it.
- Use `$humanizer` once for the final prose pass on English human-authored documentation and `$humanize-korean` for Korean prose. Preserve meaning, facts, code, commands, identifiers, URLs, citations, quotes, legal text, and generated content. Route mixed-language prose by block; leave the draft unchanged when the skill is unavailable or validation fails. Keep Korean skill `_workspace/` output untracked and remove it after applying accepted text.
- Keep `.mulgae/**`, `.gaori/runs/**`, `.podway/runtime/**`, derived `.sorage/**`, and disposable roots as local runtime evidence. Do not cite their paths or identities as durable evidence in tracked documentation or commit messages; use a reviewed tracked `aquarium.promoted-evidence/v1` package under `evidence/aquarium/` only when a downstream consumer genuinely requires retained evidence.
- Repository-specific rules in `Project Configuration` override defaults from the referenced skills.

## Project Configuration

### Repository Index and Authorities

- Use `README.md` for the product boundary, supported components, public workflows, and top-level validation entrypoint.
- Use `docs/README.md` for the documentation profile, audience boundary, semantic role ownership, authority routing, roadmap namespace, and dossier lifecycle. `docs/roadmap/README.md` alone owns adopted Epic and Task identity and lifecycle; use `docs/specs/README.md`, `docs/todo/README.md`, and `docs/deferred-feedback/README.md` for their respective work routing.
- Use `docs/architecture.md` for runtime, Git, provenance, publication, synchronization, persistence, concurrency, and safety contracts. It is the implementation authority.
- Use `docs/operations.md`, `docs/recovery.md`, `docs/deployment.md`, `docs/cli-json.md`, and `docs/hands-on-testing.md` for their respective operational, recovery, deployment, interface, and real-environment verification details.
- Use `CHANGELOG.md` and Git history for released behavior and compatibility history, not as authority for current implementation or unimplemented future work.
- Use `Makefile` as the entry point for repository-standard generation, lint, build, and test commands.
- Read the nearest relevant implementation and tests instead of copying detailed feature behavior into this file.
- If authorities or implementation disagree, surface the mismatch and resolve it before changing behavior; do not silently choose the convenient source.

#### Project Structure & Module Organization

- `cmd/sanho` is the only entrypoint. Sanho ships a single binary and has no daemon.
- Core logic sits in `internal/{buildinfo,domain,infra,interface,usecase}`; keep new packages domain-oriented.
- Layering is enforced by `internal/architecture`: a `usecase` package must not import `infra`, and an `infra` package must not import `usecase`. `internal/interface/cli` is the only place that sees both, so it is where adapters are bound to declared ports.
- Docs live in `docs/`; optional source-distributed AI-agent guidance lives in `skills/`; runtime artifacts live in `data/` and `tmp/` (ignored); builds live in `bin/`.
- Tests: co-locate package unit tests as `*_test.go`; black-box CLI behavior in `test/cli/integration`; guidance-closure and scenario suites in `test/cli/e2e`; sync/pull flow coverage in `test/docsync`; the install check in `test/install`.

#### Language Policy

- Documentation under `docs/` and `skills/`, the root README, code, inline comments, and all CLI/HTTP interfaces must be written in English. Team communication may use the user's language. Keep this repository guidance file in English.

#### Build, Test, and Development Commands

- Require Go 1.25+ to build. Git is required at runtime; no minimum git version is enforced, though merge paths need git 2.38+ in practice.
- Build/install: `make cli-build` → `bin/sanho`; `make cli-install` (aliased by `make install`) installs it to Go's binary directory. `build-cli` and `install-cli` remain as compatibility aliases.
- The complete `make test` verification runs `test-prepare`, `test-unit`, `test-int`, and `test-e2e` sequentially.
- `test-prepare` runs generation, formatting, module verification, `docs-check`, `test-package-ownership`, `test-architecture`, vet, and lint. `test-unit` runs the unit packages with `-race`. `test-int` builds `bin/sanho`, passes it through `SANHO_CLI_BINARY`, and runs `test/cli/integration` and `test/docsync`. `test-e2e` drives the built binary through `test/cli/e2e` (the scenario matrix, process-level concurrency, and the guidance-closure suite) and runs the `go install` check in `test/install`.
- Adding a package under `cmd/...` or `internal/...` requires adding it to `UNIT_PACKAGES` in the `Makefile`, or `test-package-ownership` fails.
- `make docs-check` asserts that the documented file set exists and greps for retired references; keep it green.

#### Coding Style & Naming Conventions

- Use standard Go formatting (`go fmt ./...` is in prep targets); exported names follow Go casing, packages stay lowercase.
- Keep names explicit about intent and side effects; command and hook wiring in `internal/interface/cli`, flow orchestration in `internal/usecase`, pure decisions in `internal/domain`, git execution in `internal/infra/gitx`.
- Every user-facing string that names a next command belongs in `internal/interface/cli/messages.go` and in that file's guidance catalog, not at its call site. A unit test parses the file as source and fails the build when a message advises a command without a catalog entry, and the `test/cli/e2e` closure suite then runs that command in the state the message is printed in.
- Tests use `TestXxx`/`BenchmarkXxx` patterns; table tests for branch-heavy logic are preferred.

#### Testing Guidelines

- Add unit tests near new code; move cross-adapter flow cases to `test/docsync` and black-box command behavior to `test/cli/integration`.
- Do not mock below the git boundary. Merge, publication, base re-derivation, and marker-detection logic are tested against real `git` in temporary repositories.
- Point `SANHO_CLI_BINARY` at a fresh build for the CLI suites; `make test-int` does this. Use an isolated `SANHO_HOME` in anything that touches the registry.
- Keep failing tests that capture expected behavior when fixing regressions; aim for coverage on new branches.

### Commit Messages

- Commit style matches history: `[TYPE] Brief summary (#issue-or-PR)` (e.g., `[BUG-3] Fix pending fix merge edge case (#42)`); one logical change per commit.

#### Pull Requests

- PRs outline scope, validation steps, config/env changes, linked issues; include screenshots only when output matters.
- Call out deferred follow-ups explicitly so they can be tracked.

### Project-Specific Operating Rules

- Treat `.podway/procedures/aquarium-*-v2.yaml` as this repository's authority for normal workflow evidence and routing.
- Route long or noisy checks through the configured Gaori command IDs: `prepare`, `unit`, `integration`, `e2e`, and `all`. For a dynamically selected Go test, use `gaori --json run --parser go-test --tag go --tag unit -- go test <package> <test arguments>` and give narrower Make subtasks parser and phase tags matching their output.
- Mulgae reviews require explicit user authorization. Approval of a `$aquarium:task-handler` plan counts as task-scoped authorization only when that plan explicitly includes the Mulgae review.
- A full Mulgae review uses the configured `logic`, `security`, `maintainability`, `product`, `documentation`, and `testing` roles. Its objective states the task goal, authoritative requirements, relevant invariants, expected failure boundaries, and desired validation focus.
- Do not commit, push, release, install binaries, or operate a real remote without explicit authorization.
- Do not discard, overwrite, unstage, or otherwise disturb unrelated user changes or Git operation metadata.
- Never bypass Sanho or Git safety guards with `--no-verify`, force operations, manual metadata deletion, or direct mutation of Sanho-managed state.
- Use the checkout-built `bin/sanho` with an isolated `SANHO_HOME` when validation must prove current source behavior.
- Prefer disposable Git repositories under a temporary directory for integration, hook, and real-remote fixtures. Do not point tests at production-like repositories unless the user explicitly selects them.
- Never edit generated code manually. Change its source and use the documented generator or `Makefile` target.
- Keep completion reports compact: state the outcome, changed files, verification performed, and actionable remaining risks or blockers.

#### Release Verification

Aquarium release notes: CHANGELOG.md

Aquarium release notes migration: The v0.2.8 preparation may normalize
completed sections through v0.2.7 to the canonical category schema while
preserving their semantic release history. After that migration commit, the
normalized completed sections are the byte-preserved baseline.

- When the user explicitly requests a release, ask whether to use the full gate or the reduced patch-release gate before running release checks.
- The full gate runs the configured Gaori `all` command, equivalent to `make test`. A release still requires the hands-on verdict in `docs/hands-on-testing.md`.
- The reduced gate is available only when the candidate already passed both `make test` and the hands-on release verdict, and every later change is limited to the patch version and release metadata. Ask separately whether each prerequisite passed. The hands-on prerequisite is satisfied when H01 through H08 pass or the release owner explicitly accepts every scope-based skip, as the existing verdict defines.
- Use the reduced gate only after two explicit affirmative answers. Run the configured Gaori commands `prepare`, `unit`, and `integration` in that order, equivalent to `make test-prepare`, `make test-unit`, and `make test-int`. Do not rerun `test-e2e` or the hands-on cases for this metadata-only finalization.
- If either answer is negative or uncertain, or if product code or behavior changed after the recorded evidence, do not use the reduced gate. Complete the full automatic gate and any missing hands-on work instead.
- After either gate, build the candidate with `VERSION=vX.Y.Z make cli-build`. Require `bin/sanho version` to print exactly `sanho vX.Y.Z` and `bin/sanho version --json` to print exactly `{"name":"sanho","version":"vX.Y.Z"}` before releasing.
- Any failed gate or version check blocks the release. Do not commit, tag, push, or create the GitHub release until the selected verification path passes.

#### Security & Configuration Tips

- Do not commit secrets; `.sanho*`, `data/`, and temp repos should stay untracked (init updates `.gitignore`).
- Preserve the existing permission discipline: the sanho home is `0700`, the registry and its backup are `0600`, and every state write goes through the shared atomic writer in `internal/infra/fsx`.
- Keep git invocations argv-only through `internal/infra/gitx`. Never build a shell command line, and never drop `GIT_TERMINAL_PROMPT=0` or the network runner's SSH `BatchMode` policy.
