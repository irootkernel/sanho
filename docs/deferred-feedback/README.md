# Deferred Feedback

This index owns small actionable findings intentionally postponed from current
work.

## Shared object verification cost

- **Owner:** `internal/infra/appgit` Git readers.
- **Impact:** Full blob verification preserves trustworthy read results, but
  hooks and interactive commands also pay a cost proportional to docs content.
  The existing 1,000-file, 52 MB scale scenario passes; per-command overhead
  has not been measured.
- **Re-entry:** When larger docs repositories or reported hook/command latency
  justify investigation, benchmark the affected readers and their callers.
  Use those measurements before proposing a narrower verification boundary;
  preserve missing/corrupt-object detection wherever a verdict depends on it.

## Structured diagnostic documentation drift

- **Owner:** The CLI error contract in `docs/cli-json.md` and
  `internal/interface/cli/sync_diagnostics.go`.
- **Impact:** The current reason table agrees with the implementation and
  behavioral tests. Future vocabulary changes could leave the documentation
  behind because the table is checked manually.
- **Re-entry:** When the diagnostic vocabulary grows or a mismatch is found,
  assess a focused documentation consistency check. Keep behavioral contract
  tests and real recovery scenarios; matching prose cannot replace them.

## Workspace discovery convergence

- **Owner:** Workspace discovery in `internal/interface/cli/ports.go` and
  `internal/interface/cli/inspection_workspace.go`.
- **Impact:** Inspection uses a strict runner and preserves whitespace in Git
  paths. Ordinary discovery has older parsing and error-classification rules.
  Keeping separate implementations makes future path fixes easier to miss.
- **Reason for deferral:** Inspection's current contract is verified. Sharing
  discovery also requires deciding how ordinary commands handle discovery
  failures and unusual worktree paths, beyond the inspection delivery.
- **Re-entry:** When ordinary worktree-path behavior is revised, consolidate
  parsing behind an injected runner and verify both policies, including linked
  worktrees with trailing whitespace and operational Git failures.

## Legacy sync mode error codes

- **Owner:** `syncFlags.mode` in `internal/interface/cli/sync.go` and the CLI
  error contract in `docs/cli-json.md`.
- **Impact:** The pre-existing abort/continue/rebase mode conflicts return
  `internal` in JSON, while inspection conflicts return `invalid_arguments`.
- **Reason for deferral:** Inspection preserves existing command error codes;
  changing the legacy cases needs a separate compatibility decision.
- **Re-entry:** When legacy argument classification is revised, wrap these
  errors with the invalid-argument sentinel and verify their JSON codes,
  unchanged human messages, and rejection before workspace effects.

## Completion observation provenance

- **Owner:** `CompletionAssessment` in `internal/usecase/docsync` and the
  inspection binding in `internal/interface/cli/sync_inspect.go`.
- **Impact:** Inspection currently identifies an assessment's HEAD observation
  through the named `entry_history` check. Existing checks and concurrency
  tests verify this relationship, but a future assessment redesign could
  change when HEAD is read without updating the binding.
- **Reason for deferral:** The current assessment and bounded observation
  contract are consistent; no present correctness gap requires a new API.
- **Re-entry:** When changing assessment ordering or check names, consider
  explicit observation provenance and retain the intermediate HEAD-change
  regression test.

Each entry must identify the finding, owner, reason for deferral, and concrete
next action. Work required for current correctness or acceptance cannot be
deferred. Promote epic-sized work to a TODO candidate or an adopted roadmap
unit instead of recording it here.
