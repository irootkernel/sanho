# EPIC-003: Structured Diagnostics and Sync Inspection

This is the temporary implementation dossier for
[EPIC-003](../roadmap/README.md#epic-003-structured-diagnostics-and-sync-inspection).
The roadmap alone owns Epic/Task identity, order, and lifecycle status.
Checkboxes below are implementation acceptance evidence, not a second status
register. A checked item under **Do not** confirms that the prohibition was
respected. Adopting this plan does not claim implementation or authorize a
commit, push, installation, release, or live-remote operation.

## Objective

Deliver the two approved improvements as one feature: typed recovery diagnoses
for existing synchronization refusals, and a read-only `sanho sync --inspect`
that explains the actual local prerequisites for `sync --continue`.

An operator or coding agent must be able to tell why completion is blocked,
which paths are involved, and which existing recovery guidance applies without
parsing human prose, retrying a mutation to discover its failure, or editing
Sanho-managed state. Inspection and completion must share the same decision
assessment, not two approximations of the rules. Inspection uses strictly
non-executing readers; when they cannot safely obtain a required fact, it
reports an availability error rather than guessing a completion verdict.

## Authorities and baseline

- Public target contract: [CLI diagnostics](../cli-json.md#sync-inspection).
- Implementation target: [assessment architecture](../architecture.md#structured-diagnostics-and-sync-inspection).
- Current safety and recovery: [architecture](../architecture.md) and [recovery](../recovery.md).
- Existing operational boundary: [operations](../operations.md) and the source-distributed [skill](../../skills/use-sanho/SKILL.md).
- Verification entrypoint and package ownership: [Makefile](../../Makefile).

The inspected adoption baseline is commit `d56af09`. Before implementation,
re-read relevant current code and preserve unrelated changes. This dossier is
not authority to restore the baseline over newer work.

Existing implementation anchors:

| Area | Current owner | Reuse obligation |
|---|---|---|
| Completion guards and accepted drift | `internal/usecase/docsync/docsync.go` | Preserve Continue's guard order, local-side resolution, legacy behavior, and path-preservation proof |
| Sync note representation | `internal/infra/wsstate/wsstate.go` | Read existing active notes without a schema migration |
| CLI binding and errors | `internal/interface/cli/sync.go`, `errors.go`, `ports.go` | Keep local inspection separate from clone-ensuring sync setup and retain existing sentinel/code relationships |
| Recovery guidance | `internal/interface/cli/messages.go` | Extend the single catalog rather than duplicating recovery prose or policy |
| Git and comparable trees | `internal/infra/appgit/`, `internal/infra/gitx/` | Preserve literal paths and conversion semantics; harden HEAD/object reads and enforce the inspection-only execution boundary |
| Regression evidence | `internal/usecase/docsync/`, `internal/interface/cli/`, `test/docsync/`, `test/cli/integration/`, `test/cli/e2e/` | Extend real-Git and guidance-closure coverage instead of replacing it with prose checks |

TASK-005 remains a separate existing work item. Its recovery-guidance changes
are inputs to preserve, not implementation of this Epic. Do not reopen or
complete TASK-005, EPIC-001, or EPIC-002 as part of this work. No runtime
implementation dependency on TASK-005 acceptance is introduced; the final
source-skill update must reconcile with whatever its current reviewed text is.

## Review resolutions and fixed implementation choices

The planning review identified three required corrections. They are part of
this Epic's acceptance work, not deferred follow-ups or new Task identities.

| Finding | Required implementation | Task owners | Scenario evidence |
|---|---|---|---|
| Scratch index does not prevent external filter effects | Protect the whole inspection read path, including `DocsClean()` and `WorktreeDocsTree()`; implement the architecture's conservative filter admission and explicit unavailability error | TASK-006, TASK-007, TASK-008; integrated verification in TASK-009 | S10, S17, S20-S22 |
| Failed Git reads can look like unborn/empty state | Distinguish absent refs from missing/invalid objects; fix `HeadCommit()`, `HeadDocsTree()`, scratch seeding, and relevant subtree reads | TASK-006; CLI verification in TASK-008/009 | S13 and its required subcases |
| Documented marker scope differs from implementation | Correct the current architecture to whole-docs worktree scanning; preserve that behavior in the shared assessment | TASK-006 and TASK-008; precedence verification in TASK-009 | S05 |

The architecture and CLI contract own the detailed rules. The implementation
choices are fixed for this bounded delivery:

- Do not execute configured programs during inspection. At the first reached
  worktree read that can invoke a filter, any non-empty effective clean/process
  command yields `inspection_unavailable` with `external_filter_configured`.
  This intentionally includes unused drivers and global configuration; an
  attribute-aware exemption mechanism is not required by this Epic.
- Apply inspection-only runner controls for optional index writes, fsmonitor,
  external diff/textconv, maintenance, and lazy fetch. A control that cannot be
  established yields `execution_policy_unavailable`, not an unsafe fallback.
  Failure to read configuration is an ordinary error. Do not edit user config.
- On admitted inputs, retain the existing worktree normalization semantics
  through a policy-aware scratch path. Do not replace it with raw bytes or an
  assumed-equal HEAD tree. Continue keeps its existing filter support; only
  verdicts successfully evaluated by both surfaces are required to match.
- Empty history/index/tree fallbacks require positive evidence of absence.
  Preserving valid input behavior does not preserve the existing concealment
  of corruption, missing objects, or failed Git execution.
- The recorded conflict set limits allowed merge drift, not marker scanning.
  Whole-docs markers still win before clean-docs and preservation checks.

## Scope and acceptance requirements

- [ ] SDI-01: Supported sync refusals expose the additive typed `reason`,
  `paths`, and `recovery_id` contract while preserving their existing code,
  message meaning, exit behavior, and stdout/stderr separation.
- [ ] SDI-02: Path-bearing diagnoses retain exact, sorted repository-relative
  paths as data. Commas, spaces, newlines, Unicode, and literal glob characters
  must not become parsing delimiters or pathspec expansion.
- [ ] SDI-03: One non-writing assessment supplies the ordered completion
  decision and evidence through mode-appropriate readers. Inspection cannot
  execute configured programs; Continue's existing conversion support remains.
  Hook resolution heuristics are not substituted for the completion proof.
- [ ] SDI-04: `sanho sync --inspect [--json]` distinguishes no note, corrupt
  note, active sync, and detected observation changes; it reports the first
  actual blocker and marks unevaluated checks honestly. Policy-limited
  assessment instead returns the specified availability error and exit 1.
- [ ] SDI-05: Inspection exposes the recorded entry/target/merge identities and,
  when evaluated, exact allowed versus unexpected path changes. A note target
  is never presented as an already adopted base.
- [ ] SDI-06: Inspection is local and non-mutating under the architecture's
  scratch and external-execution policies, including status/cleanliness reads.
  It does not create side effects through filters or fsmonitor, require/create/
  repair/fetch a canonical clone, update a registry, or run a commit/push/hook.
- [ ] SDI-07: Inspection and Continue agree on unchanged fixtures where the
  strict readers can evaluate the required facts, including local-side
  resolutions with no new commit and accepted unrecorded legacy entry history.
  A missing merge tree still blocks completion. Availability errors are not
  completion refusals and must not newly reject filter use by Continue.
- [ ] SDI-08: Existing failure guards and persistence order remain intact;
  fix false unborn/empty success fallbacks on HEAD/object/read failures.
  Readiness is not a guarantee of a later write and never bypasses a fresh
  assessment, base guard, or authorization boundary.
- [ ] SDI-09: Human guidance and machine recovery IDs use one CLI catalog with
  executable closure coverage. No agent must parse English to identify the
  newly distinguished refusal reasons.
- [ ] SDI-10: Existing clients continue to work with unchanged success shapes
  and old two-field error envelopes. New consumers tolerate either error form.
  Hook entrypoints gain no JSON stdout protocol.
- [ ] SDI-11: Machine state and source guidance describe conditional inspection,
  not a mandatory extra command on every commit/push. Already completed work
  is not repeated merely because a diagnostic reports a blocker.
- [ ] SDI-12: Focused, integration, failure-path, and full repository verification
  pass with explicit reporting of any unperformed manual agent checks.

## Non-goals

Do not add a daemon, Web UI, workflow/session runtime, journal, receipt store,
automatic repair, automatic conflict resolution, new `sanho push`, global
workspace scanning, publication patch preview, or a policy language. Do not
add multi-directory synchronization, persistent-state migration, hook
installation changes, or a release-version bump.

The first inspection reports tree identities and path-level differences. A
file-content/patch viewer, recovery plan execution, and new automatic commands
are outside this Epic. Existing `status`, `preview`, and `check` retain their
current purpose and success documents. A filter interpreter, allowlist of
trusted scripts, OS sandbox, or attribute-scoped filter exemption framework is
not part of the initial inspection policy. Do not widen this work into a new
Git runtime or an audit of every unrelated Git helper.

## Execution order

Implement one Task at a time in this order:

`TASK-006 -> TASK-007 -> TASK-008 -> TASK-009 -> TASK-010`

Each Task is one cohesive commit-sized delivery unit when a commit is
separately authorized. Its verification belongs with the implementation, not
only in the later regression Task. If discovered work no longer fits the
bounded task, reconcile the roadmap before widening the scope; do not hide a
second feature in the implementation.

### TASK-006: Extract the shared read-only completion assessment

**Goal:** Give Continue and its future diagnostic reader one typed assessment,
preserving valid-input completion behavior while fixing false success from
failed reads and providing the strict inspection execution boundary.

Implement:

- [x] Add assessment/result and blocker types in the existing docsync package;
  keep ports read-focused and avoid a new framework or CLI dependency.
- [x] Extract note, marker, clean-docs, ancestry, merge-tree, and non-conflict
  preservation checks from Continue, retaining first-failure precedence.
- [x] Return structured path evidence and expected/actual comparable trees;
  preserve existing error sentinels through wrapping or typed causes.
- [x] Make Continue use a fresh assessment before its current mutation phase.
  Preserve clear-note-before-guarded-base-write ordering and write failures.
- [x] Provide the read boundary needed for the inspection-only observation
  check without accepting a previous read as a completion token.
- [x] Keep missing legacy entry fields distinct from a missing merge tree.
- [x] Introduce bounded inspection reader policy in the existing adapters,
  covering `DocsClean()` as well as `WorktreeDocsTree()`. Read effective Git
  configuration without programs; implement conservative filter admission
  before each reached filter-capable read and enforce the runner controls.
  Carry a typed unavailability cause without importing CLI codes into usecase.
- [x] Preserve current Git normalization on admitted inputs. Keep Continue's
  normal filter-aware readers; do not turn inspection restrictions into new
  completion guards or raw-byte comparisons.
- [x] Harden `HeadCommit()`, `HeadDocsTree()`, scratch seeding, and their used
  tree/object readers. Verify unborn versus detached/broken/missing-object HEAD;
  only proved absence permits an empty fallback. Use Git ref APIs compatible
  with packed refs and linked worktrees; propagate capability/execution errors.
- [x] Preserve the scanner's whole-docs regular-file traversal, including
  untracked/ignored files and paths outside recorded conflicts. Align source
  comments and architecture; do not narrow the scanner to match the old prose.

Do not:

- [x] Use `ResolutionState == resolved` as a new completion requirement.
- [x] Change the conflict set, marker scope, or accepted local-side resolution.
- [x] Expose the new CLI mode before its command contract and tests are ready.
- [x] Add state writes, registry calls, clone setup, or network to assessment.
- [x] Treat `--no-optional-locks` or a scratch index as filter isolation, or
  silently trust an unguarded nested Git invocation.
- [x] Translate a failed HEAD/object read to unborn, or retry failed
  `read-tree HEAD` with `--empty` without proving unborn state.

Verify and finish:

- [x] Table-test every ordered blocker, skipped check, and successful drift
  result; prove assessment invokes no mutation capabilities.
- [x] Retain real-Git regression cases for local-side resolutions, ancestry,
  discarded clean upstream content, legacy notes, and comparable-tree semantics.
- [x] Add reader-level regression cases from S13 and S20-S22 now, including
  unchanged contents with changed mtime, scratch re-staging, external sentinel
  files, Git execution controls, and built-in conversions. Do not postpone
  adapter safety to TASK-009 or accept index-only snapshots as proof.
- [x] Assert a marker in a regular file outside the recorded conflict set wins
  over dirty docs and any later inspection filter restriction.
- [x] Inject note-clear and guarded-base-write failures and verify the existing
  ordering and actual residual state, not an invented rollback guarantee.
- [x] Existing Continue tests pass without changing expected safety behavior.
  Record exact checks and outcomes before marking the Task complete.

### TASK-007: Add structured error details through the guidance catalog

**Goal:** Let machines distinguish the approved sync refusal families while
existing consumers keep their code/exit behavior.

Implement:

- [x] Add the planned optional error fields for the reason table in CLI JSON.
- [x] Carry path evidence through typed errors; never split human strings.
- [x] Define public reason/recovery mappings at the CLI boundary and associate
  recovery IDs with the existing guidance catalog entries.
- [x] Cover active/corrupt sync refusals from sync/pull and the listed Continue
  causes. Leave unrelated error envelopes and success documents unchanged.
- [x] Add the inspection-only `inspection_unavailable` mapping with
  `external_filter_configured` / `execution_policy_unavailable`, exit 1, empty
  paths, and null recovery. A fixed CLI message explains assessment limits
  without inventing a completion verdict or an automatically runnable remedy.
- [x] Promote only the delivered existing-command error-detail subsection into
  the current machine contract. The new inspection mode and its availability
  error remain planned until TASK-008 delivers that public surface.

Do not:

- [x] Replace `error.code`, invent new success exit rules, or emit hook JSON.
- [x] Populate a specific diagnosis from only a generic code such as
  `sync_in_progress` or `docs_dirty`.
- [x] Add a parallel recovery table to the skill or auto-execute catalog steps.
- [x] Relabel policy-limited inspection as `docs_dirty`, note corruption, or
  an internal defect, or change an existing command's error code to the new
  inspection-only code.

Verify and finish:

- [x] Test old consumer decoding of extended envelopes and new consumer
  fallback to existing two-field envelopes.
- [x] Assert stable sentinel/code/exit behavior, single stdout JSON, stderr
  guidance, null recovery where required, and unchanged version/success output.
- [x] Verify special filenames survive the entire typed-error-to-JSON path.
- [x] Extend catalog identity/closure checks for the new recovery IDs and
  preserve the existing command/prerequisite scenarios.
- [x] Test the two availability mappings, null-recovery meaning, absence of
  command/config secrets, and one error envelope with no partial success JSON.

### TASK-008: Deliver local read-only sync inspection

**Goal:** Provide `sanho sync --inspect [--json]` using the shared assessment.

Implement:

- [x] Add the mutually exclusive inspection mode and argument validation before
  clone setup, mutation, or network calls.
- [x] Bind TASK-006's strict application and worktree-local readers without
  the normal sync clone-ensuring constructor or registry updates. Apply the
  execution policy throughout nested reads, not just to final tree comparison.
- [x] Stop with TASK-007's availability error when a reached fact cannot be
  obtained under that policy. Preserve earlier no-note, corrupt-note, or marker
  diagnoses instead of running later probes or replacing their precedence.
- [x] Render the exact planned JSON shape and an equivalent readable report.
  Mark absent, unknown, skipped, corrupt, and changed observations explicitly.
- [x] Expose the first blocker, recorded conflict paths, and allowed/unexpected
  path differences when comparison is reached.
- [x] Compare HEAD and note identity/content around assessment, invalidate a
  detected change, and keep observation bounded without automatic retry loops.
- [x] Use the shared catalog only where guidance is needed. Quote unusual paths
  safely in text and preserve them losslessly in JSON.
- [x] Promote the implemented inspection CLI contract and architecture section
  and add conditional usage to operations/recovery without claiming final Epic
  acceptance.

Do not:

- [x] Treat a missing note as a failed diagnostic, or a corrupt note as absent.
- [x] Return a successful empty/ready report for Git, I/O, missing-object, or
  scan-limit failures that prevent assessment.
- [x] Change the ordinary sync success schema or add refresh/apply/repair flags.
- [x] Require a canonical clone or adopt the note's target while inspecting it.
- [x] Disable user filters and then return a guessed verdict, run a configured
  program even on an error path, or fall back to ordinary status/sync/Continue.

Verify and finish:

- [x] CLI integration tests cover none, ready, each blocker, corrupt/legacy
  notes, changed observation, and invalid combinations including an empty
  supplied rebase target.
- [x] Inspect with an unavailable remote and with the private clone removed
  from an isolated fixture whose required local objects remain available.
- [x] Snapshot protected files, refs, index, and Git metadata before/after
  inspection, including error paths and a linked-worktree fixture.
- [x] Human and JSON outputs agree; blocked inspection exits 0 while actual
  refused Continue retains the old nonzero error path. Unavailable inspection
  exits 1 with its error document and no readiness claim.
- [x] Extend CLI fixtures with the S13 read-failure subcases, S05 markers
  outside recorded conflicts, and S20-S22 indirect-execution probes. Assert
  external sentinel files and protected state remain unchanged on success,
  diagnostic refusal, operational error, and cancellation paths.

### TASK-009: Verify cross-surface parity and failure boundaries

**Goal:** Prove that diagnostics explain the real completion behavior without
weakening it or adding hidden effects.

Implement and verify:

- [x] Complete the scenario matrix below using package tests and real-Git
  integration/E2E fixtures; reuse existing fixtures instead of duplicating an
  unrelated harness.
- [x] Compare inspection and Continue on equivalent independent fixtures, or
  inspect then Continue only where the inspection's no-mutation proof makes
  that comparison valid. Do not reuse a fixture after a mutation as if its
  earlier state still existed. Limit verdict parity to safely assessed inputs;
  separately prove that a filtered fixture returns inspection unavailability
  while authorized Continue still uses its original normalization semantics.
- [x] Test precedence with multiple simultaneous blockers and operational
  failures; a skipped check must never be reported as passed.
- [x] Test changed HEAD/note observations and a separate edit between a ready
  inspection and Continue; the mutation must reassess rather than trust output.
- [x] Assert protected-state preservation, worktree isolation, no network or
  clone repair, and unchanged persistence failure semantics. Include sentinel
  paths outside the repository for clean/process/fsmonitor/diff/textconv probes;
  a byte-identical real index alone is insufficient.
- [x] Verify clean-filter and long-running process-filter fixtures are capable
  of invocation in disposable positive controls. Reset only fixture-owned
  evidence after setup, then prove inspection launches none of those programs.
  Force same-content mtime changes so cached status cannot hide the risk.
- [x] Exercise missing commit/tree/blob objects, corrupt index, valid unborn
  and detached HEAD, packed refs, and linked worktrees; prove no false empty
  assessment, no lazy fetch, no note/base mutation, and no silent repair.
- [x] Verify actual catalog-recommended recovery sequences under their stated
  prerequisites, including abort/restart preservation of committed work.

Do not:

- [x] Weaken an assertion to make a failed safety case pass.
- [x] Treat prose matching, compilation, or a mocked Git merge as adequate
  evidence for actual Git, publication, or persistence boundaries.
- [x] Operate a real remote, install globally, or run an unapproved review tool.

Done when:

- [x] Every applicable matrix row has a test owner and recorded passing
  evidence; existing sync-window, guard, concurrency, and closure regressions
  still pass. Report unresolved failures instead of marking the Task complete.

### TASK-010: Update consumer guidance and prepare Epic acceptance

**Goal:** Finish the source guidance and evidence handoff without confusing
implemented capability, installed capability, and user authorization.

Implement:

- [ ] Update the source skill and only its relevant recovery/inspection
  references to branch on typed reasons when present and retain current
  fallback behavior for an older installed binary. An unsupported `--inspect`
  flag must never fall back to running mutating `sanho sync`; use existing
  supported read-only evidence and report the capability limit instead. This
  old-binary fallback is not permission to bypass an execution-policy refusal:
  on `inspection_unavailable`, stop and report the limitation without retrying
  through ordinary status/sync, changing filters, or invoking Continue.
- [ ] Make inspection conditional on an active-sync diagnosis or uncertainty.
  Do not require it in the ordinary successful commit/push loop.
- [ ] Preserve TASK-005's refusal-specific recovery and partial-push guidance;
  update current documents rather than copying conflicting instructions.
- [ ] Remove delivered planned labels, integrate the implemented schema and
  invariants into their canonical sections, and remove obsolete duplication.
- [ ] Extend the existing agent-verification guide with bounded cases for
  structured recovery, blocked versus unavailable inspection, old-binary
  fallback, and approval boundaries. Explain the conservative configured-filter
  limit and preserved Continue support. Do not silently install the edited skill.

Verify and finish:

- [ ] Run `make docs-check`, focused changed-area tests, and the complete
  repository `make test` gate through the configured execution policy.
- [ ] Use a fresh checkout-built binary and isolated `SANHO_HOME` for CLI
  scenarios. Record command, tested revision, exit/result, and failure fixes.
- [ ] Report manual agent cases as performed, unperformed, or explicitly
  accepted skips; documentation and fixtures alone are not observed agent
  behavior. Do not claim a release or installation from these tests.
- [ ] Present acceptance against SDI-01 through SDI-12 and prepare the proposed
  canonical outcomes/closeout changes. Complete this Task's handoff before
  requesting Epic acceptance; leave the dossier available for that review.
  Member Task completion does not automatically complete the Epic.

## Scenario matrix

Each row may map to more than one focused test. Add behavioral assertions in
the owning Task before the final combined gate. Successful verdict parity
assumes the required facts are safely evaluable; S20-S22 also prove explicit
unavailability without imposing the inspection restriction on Continue.

| ID | Scenario | Required result |
|---|---|---|
| S01 | Committed resolution changes only recorded conflicts | Inspection ready; Continue completes with the same drift count |
| S02 | Valid resolution keeps the local side with no new commit | No heuristic commit requirement; original safety checks decide readiness |
| S03 | No sync note | Inspection `none`/`no_sync`, exit 0; Continue keeps its refusal code |
| S04 | Malformed note or invalid completion target | Inspection `corrupt` with the corresponding reason; no repair or state deletion |
| S05 | Markers in a regular docs file outside recorded conflicts, including untracked/ignored cases; dirty docs and later filter/merge blockers coexist | Whole-docs `markers_remaining` wins with exact paths; later checks/probes do not run; symlinks and binary rules remain unchanged |
| S06 | Staged, unstaged, or untracked docs changes | Uncommitted-resolution blocker; unrelated non-docs work is preserved |
| S07 | HEAD is on unrelated history | Foreign-history blocker; no base advancement |
| S08 | Committed resolution changes a non-conflicting path | Exact unexpected paths; refused Continue preserves note and base |
| S09 | Legacy missing merge tree; separately, accepted missing entry history | First remains blocked; second is not misreported as proved ancestry or newly rejected |
| S10 | Binary files, symlinks, modes, tracked-but-ignored files, deletion/rename, built-in EOL/encoding/ident conversions without external filters | Admitted worktree comparison retains the same literal-path, normalized tree, and drift result as actual completion; no raw-byte or HEAD-tree shortcut |
| S11 | Custom docs directory and filenames with commas/newlines/Unicode/globs | Correct repository-relative, lossless paths in JSON and safely quoted text |
| S12 | Canonical offline or clone missing with sufficient local objects | Inspection works locally without a network call or clone creation |
| S13 | Positively verified unborn HEAD versus broken/missing-object HEAD, missing required tree/blob, corrupt index, Git/I/O failure, or scan-size limit | Only proved absence permits empty state; failures retain normal error classification and never produce empty-index/tree fallback or ready evidence; required subcases below |
| S14 | Inspection combined with another sync mode | `invalid_arguments` before effects, including supplied empty rebase target |
| S15 | HEAD or note changes during inspection | `changed` result with invalidated evidence and no readiness claim |
| S16 | Docs/HEAD/note changes after a ready inspection | Continue performs fresh checks; old JSON is not an execution token |
| S17 | Linked worktrees, protected-state snapshots, and fixture-owned external sentinels | Correct worktree note; preserve both worktrees, real indexes, shared clone, registry, refs, and external paths; only admitted bounded scratch effects are allowed |
| S18 | Error compatibility and persistence failure injection | Stable old code/exit/success shapes; preserve note-clear/base-write failure behavior |
| S19 | Catalog recovery and agent approval boundaries | Same human/machine guidance identity; prescribed steps are verified but never implicitly authorized |
| S20 | Configured clean and process filters on docs; same-content mtime change and scratch re-staging positive controls | Before status/add can invoke a filter, inspection exits 1 with `inspection_unavailable` / `external_filter_configured`; external sentinels unchanged; equivalent Continue fixture retains filter-aware behavior |
| S21 | fsmonitor hook/service, external diff/textconv, optional index refresh, automatic maintenance, or promisor-object lazy fetch could run | Enforce inspection controls without user-config changes; no prohibited program/network effect; inability to enforce is explicit unavailability, while missing-object/execution failures remain errors |
| S22 | Global/includeIf/worktree/env filter configuration, unused configured driver, config-read failure or detected configuration change | Conservative admission includes unused drivers and effective configuration scopes; config errors do not look like no filters, detected changes stop safely, and no fallback executes a program |

### Required S13 subcases

- Fresh initialized repository with a valid symbolic HEAD and absent branch:
  classify unborn positively. Use a controlled note fixture where needed;
  absence of a note does not bypass reader-level classification coverage.
- Existing branch ref with the HEAD commit object removed in a disposable
  repository, both loose and packed-ref cases: return an error, not unborn.
- Valid detached HEAD versus dangling/malformed detached HEAD and invalid
  symbolic references: only the valid detached case yields committed identity.
- Existing commit with its required root/docs tree or a required blob missing;
  a genuinely absent docs subtree in a verified tree is the separate valid
  empty case. Accessing missing objects must not trigger remote repair.
- Corrupt/unreadable index or a failed `read-tree` after verified HEAD:
  propagate failure; do not recover by running `read-tree --empty`.
- Git launch failure, cancellation, non-absence Git failure, and I/O failure:
  no conversion to empty history, empty path lists, or success. Use real Git
  repositories for corruption cases and narrow process fault injection for
  launch/error paths, not mocks of the merge semantics.

### Scenario test owners

The following owners cover the matrix. CLI fixtures use the checkout-built
binary, isolated homes, and disposable repositories. Inspection precedes
Continue only after protected-state snapshots prove that the fixture is
unchanged. Filter cases instead compare explicit inspection unavailability
with successful filter-aware completion.

| Scenarios | Behavioral test owners |
|---|---|
| S01, S03, S04, S07, S08 | `test/cli/e2e/inspection_test.go`: `TestSyncInspectionDiagnosesLocalCompletion`, with actual Continue assertions in `inspection_parity_test.go` |
| S02, accepted S09 | `TestSyncInspectionLocalAndLegacyParity`: unchanged HEAD, retained staged/unstaged code, legacy ancestry marked `not_applicable` |
| S05 | `test/docsync/assessment_test.go`: `TestAssessmentWholeDocsMarkersPrecedeFilterAdmission`; `test/cli/e2e/inspection_test.go`: `TestSyncInspectionWholeDocsPathsAndFilterPrecedence`; worktree scanner tests in `internal/infra/appgit/write_test.go` |
| S06 | `TestSyncInspectionDirtyDocsParity` covers staged, unstaged, and untracked docs; `TestSyncInspectionLocalAndLegacyParity` preserves unrelated code |
| Missing-tree S09 | `TestSyncInspectionDiagnosesLocalCompletion/missing_merge_tree` retains the refusal and note/base bytes |
| S10 | `TestSyncInspectionGitContentParity`: binary, symlink, mode, tracked ignored content, rename/deletion, EOL, UTF-16LE and ident normalization; actual Continue matches tree and drift |
| S11 | `TestSyncInspectionCustomLiteralPathsParity`: custom directory and conflict filename with commas, newlines, Unicode and glob characters; nearby non-docs content survives |
| S12 | `TestSyncInspectionWithoutCloneOrReachableRemote` |
| S13 | `TestSyncInspectionReadFailuresNeverBecomeReady`, `TestSyncInspectionUnbornAndDetachedHead`, `TestSyncInspectionCapabilityLaunchAndConfigFailures`, and `TestSyncInspectionWorkspaceRefusals`; object/ref/index/scratch cases in `internal/infra/appgit/inspection_test.go` |
| S14 | `TestSyncInspectionInvalidModesRunNoGit` |
| S15 | `internal/interface/cli/sync_inspect_test.go`: `TestInspectionInvalidatesChangedObservation`, including intermediate HEAD movement and same-content note replacement |
| S16 | `TestSyncInspectionReadyDoesNotAuthorizeLaterState`: docs, foreign HEAD, and invalid note changes each produce a fresh Continue refusal |
| S17 | `TestSyncInspectionLinkedWorktreePrivateNote`, `TestSyncInspectionCancellationPreservesProtectedState`, and all `inspectUnchanged` snapshots |
| S18 | `internal/usecase/docsync/assessment_test.go`: `TestContinueClearFailurePreservesNoteAndBase`; `docsync_test.go`: `TestContinueInterruptedBeforeTheBaseWriteFailsOld`; `test/cli/integration/surface_test.go`: `TestJSONErrorsCarryAMachineEnvelope` and `TestSyncJSONSchema`; actual parity assertions |
| S19 | `TestGuidanceClosure`, including `sync_continue_blocked`, `sync_continue_unverified`, `sync_note_corrupt`, `sync_continue_foreign_history`, and `sync_in_progress_command`; source-agent behavior remains a separate manual check |
| S20 | `TestSyncInspectionFilterNormalizationPreservesContinue`: real clean and long-running process filters, nonidentity conversion, add/status positive controls and mtime changes; reader scratch controls in `TestInspectionRejectsFiltersBeforeStatusOrScratch` |
| S21 | `TestSyncInspectionControlsApplyBeforeDiscoveryAndNestedReads`: separate external-diff/textconv controls; `TestInspectionBuiltinConversionsAndNoFSMonitor`, `TestObjectReadersNeverLazilyFetchMissingObjects`, and `internal/infra/gitx/inspection_test.go` |
| S22 | `TestInspectionConfigurationAdmission`: local/global/includeIf/worktree/environment, unused driver, changed and corrupt configuration; CLI config-read failure coverage |

TASK-009 verification passed on the candidate based on `34b26a9`:

- `go test -race ./internal/usecase/docsync ./internal/infra/appgit
  ./internal/infra/gitx ./internal/interface/cli`: passed through Gaori.
- `make test-e2e`: passed through the configured Gaori `e2e` command,
  including the scenario, sync-window, guard, concurrency, guidance-closure,
  and isolated install suites. The first run failed only during temporary
  directory cleanup in two existing scenarios. Three focused repetitions and
  the complete rerun passed; the remaining cleanup investigation is recorded
  in [deferred feedback](../deferred-feedback/README.md#intermittent-e2e-fixture-cleanup).
- `make test-prepare` and `make docs-check`: passed. The unchanged integration
  surfaces retain TASK-008's passing `make test-int` evidence.
- New focused parity cases passed against a fresh `bin/sanho` with isolated
  homes. The initial EOL fixture incorrectly paired CRLF worktree bytes with
  `eol=lf`; using the intended `eol=crlf` contract proves normalized LF blobs
  and successful completion without weakening the clean-docs guard.

The complete repository gate and manual agent-case reporting remain TASK-010
work. These fixtures do not claim observed agent approval behavior.

## Verification discipline and closeout

Use repository-standard Make targets and the configured Gaori command IDs for
long/noisy checks, as required by `AGENTS.md`. Run narrow checks first and the
full gate once the integrated feature is ready; rerun affected checks after a
fix. This is not an instruction to repeat unchanged reviews or gates forever.
An independent Mulgae review requires the repository's explicit authorization.

Tests must use disposable repositories and isolated homes. Do not cite ignored
runtime artifacts as durable documentation evidence or touch the user's real
remote/state to satisfy a fixture. New packages, if genuinely required, must
be added to Makefile package ownership; prefer existing domain-oriented
packages for this bounded feature.

Planning-time checks validate these documents and their links only. They do
not satisfy implementation acceptance, test the new flags, complete a Task,
or change installed Sanho behavior.

## Epic closeout after Task completion

After TASK-010's handoff is complete and Master explicitly accepts the Epic,
retain durable contracts/evidence in their canonical owners, remove this
dossier and its TODO entry, replace roadmap `Detailed SOT` with `Canonical
Outcomes`, update references to promoted sections, and then transition the
Epic. This closeout is an Epic-level acceptance step, not a prerequisite that
would make the last Task depend on the Epic already being completed.
