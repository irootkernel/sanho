# Deferred Feedback

This index owns small actionable findings intentionally postponed from current
work.

## Shared object verification cost

- **Owner:** `internal/infra/appgit` Git readers.
- **Impact:** Full blob verification preserves trustworthy read results, but
  hooks and interactive commands also pay a cost proportional to docs content.
  The existing 1,000-file, 52 MB scale scenario passes; per-command overhead
  has not been measured.
- **Reason for deferral:** No measured latency problem currently requires a
  change to the verified object-read boundary.
- **Re-entry:** When larger docs repositories or reported hook/command latency
  justify investigation, benchmark the affected readers and their callers.
  Use those measurements before proposing a narrower verification boundary;
  preserve missing/corrupt-object detection wherever a verdict depends on it.

## Structured diagnostic documentation drift

- **Owner:** The CLI error contract in `docs/cli-json.md` and
  `internal/interface/cli/sync_diagnostics.go`, together with the independently
  distributed `skills/use-sanho/references/recovery.md` and `inspection.md`.
- **Impact:** The current reason table agrees with the implementation and
  behavioral tests. Future vocabulary changes could leave the documentation
  behind because the table and source-skill reason guidance are checked manually.
- **Reason for deferral:** The current vocabulary is consistent across these
  owners; an automated drift guard is independent of current acceptance.
- **Re-entry:** When the diagnostic vocabulary grows or a mismatch is found,
  assess a focused consistency check across the contract and source skill.
  The distributed skill must retain its local references. Keep behavioral contract
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

## Intermittent E2E fixture cleanup

- **Owner:** Temporary-repository lifecycle in `test/cli/e2e/harness_test.go`.
- **Impact:** A combined E2E run passed its behavioral assertions but failed
  cleanup in `TestAnUnresolvedSyncDoesNotBlockUnrelatedCommits` and
  `TestGuidanceClosure/staged_markers/git_add_docs_git_commit`. Both failures
  left `.git/objects/info/packs` after directory removal began. The writer has
  not been identified.
- **Reason for deferral:** Three focused repetitions and the complete E2E
  rerun passed without changing assertions or runtime behavior. Current
  acceptance has passing evidence; intermittent cleanup remains independent
  test-infrastructure work.
- **Re-entry:** If cleanup fails again, capture Git child-process tracing in
  disposable fixtures and identify the writer before changing process lifetime
  or fixture cleanup. Do not hide a functional failure with cleanup retries.

## Changed inspection process coverage

- **Owner:** `runSyncInspect` in `internal/interface/cli/sync_inspect.go` and
  `test/cli/e2e/inspection_execution_test.go`.
- **Impact:** Unit tests cover seven observation changes and their human
  output, but no CLI process test produces the `changed` state. A future
  change to output or error handling could break its exit status or JSON
  envelope without those tests catching it.
- **Reason for deferral:** The current CLI renders every inspection state
  through the same path. Existing mutation tests satisfy the accepted
  freshness contract; process coverage is additional regression protection.
- **Re-entry:** When inspection output or error handling changes, add a
  deterministic Git gate that lets a fixture change HEAD or the sync note
  during inspection. Assert exit status 0, `changed`, invalidated checks,
  null recovery, and no protected-state writes beyond the fixture's mutation.

Each entry must identify the finding, owner, reason for deferral, and concrete
next action. Work required for current correctness or acceptance cannot be
deferred. Promote epic-sized work to a TODO candidate or an adopted roadmap
unit instead of recording it here.
