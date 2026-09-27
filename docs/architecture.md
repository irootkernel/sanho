# Sanho Architecture

This document is the implementation authority for the current v0.2 series. It
defines the runtime, Git, provenance, publication, synchronization,
persistence, concurrency, and safety contracts. `CHANGELOG.md` and Git history
record how the product reached this design; they do not override this document.

## Product boundary

Sanho keeps the `docs/` working copies of application repositories aligned with
one canonical, docs-only Git repository. It records which canonical commit each
workspace derives from and publishes document changes only when the user pushes
the application repository.

Sanho ships one executable, `sanho`. There is no daemon, socket, HTTP API, web
UI, session manager, or application-ref manager. Runtime requirements are Git
and credentials that can access the canonical repository.

Checkout development uses the two Make targets `aquarium-dev-describe` and
`aquarium-dev-build`. The producer reads `CurrentVersion` from
`internal/buildinfo/version.go`, accepts a clean local `main` only when its
`HEAD` equals `refs/heads/main`, and archives that commit before building. It
builds for Darwin arm64, keeps the executable and all temporary and cache files
under the required absolute empty `AQUARIUM_DEV_OUTPUT`, and emits one manifest
with the full commit SHA, development version, and executable checksum. Go
workspace and overlay settings are disabled at this boundary, so ignored and
untracked checkout files cannot enter the build. The result is still the
single Sanho binary, and `sanho version --verbose --json` reports the injected
commit identity when present.

Four principles govern every flow:

1. **Publish on push.** Canonical publication occurs only from `pre-push`.
2. **Detect on commit.** Commit hooks are local and never open the network.
3. **Never author application commits implicitly.** Sync commits are created
   only by an explicit user command; hooks never create commits or move refs.
4. **Use Git and files for coordination.** Remote compare-and-swap serializes
   publication; local state is protected with file locks.

## Runtime layout

```text
application repository
  docs/                         tracked working copy
  .sanho.json                  workspace configuration
  .sanho_base.json             canonical base pointer
  <git-dir>/sanho/sync.json    only during a conflicted sync
  <git-common-dir>/sanho/
    canonical/                 private bare clone

~/.sanho/
  state.json                   project and workspace registry
  state.json.bak               atomic backup
  state.lock                   registry lock
```

The canonical repository is docs-only: its root tree is the application
repository's `docs/` tree. Canonical content must not be nested under another
`docs/` directory.

The private clone belongs to the Git common directory and is shared by linked
worktrees. The sync note belongs to the worktree-specific Git directory so one
worktree's conflict window cannot block another.

## Package boundaries

The architecture test enforces these layers:

| Layer | Responsibility |
|---|---|
| `cmd/sanho` | Single executable entrypoint and build information |
| `internal/domain` | Pure provenance, publication, and marker decisions |
| `internal/usecase` | Publication, synchronization, and status orchestration |
| `internal/infra` | Git, filesystem, canonical clone, registry, and workspace adapters |
| `internal/interface/cli` | CLI surface and adapter binding |

`usecase` must not import `infra`, and `infra` must not import `usecase`.
`internal/interface/cli` is the only package allowed to bind both sides.
Lifecycle commands (`init`, `clean`, `migrate`, and `doctor`) remain in the CLI
layer because they coordinate concrete filesystem and Git effects.

## Provenance

Application commits may carry two trailers:

```text
docs-base: <canonical-commit>
docs-base-tree: <canonical-root-tree>
```

The commit identifies ancestry; the tree corroborates content across canonical
history rewrites. A legacy `docs-version` trailer is read only when `docs-base`
is absent. Sanho never writes the legacy key.

The commit-msg hook stamps provenance only when staged docs differ from `HEAD`.
It replaces existing Sanho provenance trailers instead of appending duplicates.
The pre-commit hook reads staged content, reports local freshness, and blocks
only staged conflict markers.

`.sanho_base.json` stores the workspace base:

```json
{"version":2,"commit":"<oid>","tree":"<oid>"}
```

A base means that the current docs were derived from that canonical state. Every
base write goes through the CLI's guarded writer. A new base is accepted only
when the worktree tree equals it, provenance supports it, it is an older safe
value, merging it is a no-op, or it completes the sync that established it.
Uncorroborated writes fail closed and require synchronization.

HEAD-moving hooks re-derive the base from commit history. If no provenance
supports the new branch, the old base is cleared rather than inherited across
unrelated history. While a sync is active, re-derivation stands down.

## Publication

`pre-push` receives Git's ref-update stream and ignores deletion-only and
tag-only pushes. For relevant branch tips it performs this sequence:

1. Reject an active sync, staged/pushed markers, an unsupported empty-docs
   deletion, or a base that cannot be resolved.
2. Fetch the canonical origin non-interactively.
3. Import each pushed application tip into the private clone without changing
   application refs.
4. Freeze one canonical snapshot and evaluate every pushed tip against it.
5. Chain accepted document trees in memory; write nothing until the whole
   multi-ref push validates.
6. Create canonical commits and publish with compare-and-swap semantics.
7. Advance the local base only when the guarded writer can corroborate it.

The publication cases are:

| Case | Result |
|---|---|
| Application docs equal canonical | `up_to_date`; no canonical commit |
| Application docs changed and canonical equals base | Fast-forward publication |
| Both changed and merge is clean | Publish the merged tree |
| Both changed and merge conflicts | Reject and require `sanho sync` |
| Recorded base disappeared but its tree matches rewritten history | Re-derive and continue |
| Recorded base disappeared and no safe anchor exists | Reject with rewrite guidance |

A fast-forward publishes the pushed tree directly over canonical head, so it
additionally requires the pushed branch's own provenance to vouch for the base:
the newest reachable `docs-base` must name it or a canonical ancestor of it.
A rewrite invalidates that commit without invalidating the derivation, so the
same stamp's `docs-base-tree` corroborates instead when it names the tree
canonical head publishes — the content half of the proof, which is what the
tree trailer exists for. Failing both, the tip must absorb canonical head
exactly. A branch carrying no provenance at all satisfies none of them and is
refused, which is the case the requirement was introduced for.

Canonical commits are linear and use:

```text
[SANHO] Publish docs from <repository>/<branch> (<N> app commits)

source: <workspace-id> @ <application-tip>
commits:
- <oid> <subject>
```

Sanho never pushes, fast-forwards, or rewrites an application ref. A failed
pre-push leaves both the canonical ref and the application remote unchanged.
Publication uses network SSH with `BatchMode=yes`, a connection timeout, and no
credential prompt. A compare-and-swap loss is refetched and retried within the
bounded publication flow; force push is never used as an escape hatch.

## Merge and marker contracts

Merges use real Git objects and `git merge-tree --write-tree`. All participating
objects are imported into one object database before the merge. A lock protects
fixed merge helper refs in the shared private clone.

Marker detection has a different scope at each boundary:

- pre-commit scans staged changed docs;
- pre-push scans docs changed by pushed commits;
- sync completion scans regular files throughout the configured docs worktree,
  including untracked and ignored files, not only recorded conflict paths.
  Deleted files are absent; symlinks and other irregular entries are skipped.

The recorded conflict set instead bounds the changes allowed by the subsequent
merge-result preservation check. It does not narrow the marker scan. A complete
marker sequence outside that set still blocks completion before clean-docs or
merge-result checks. This describes the existing worktree scanner, not a new
expansion of its scope.

Files with a NUL byte in the first 8 KiB are treated as binary and skipped.
Text files larger than the configured safety limit are reported instead of
being loaded without bound. A start marker alone is not a conflict; a complete
start/separator/end sequence is required.

## Synchronization

`sanho sync` fetches canonical, imports its objects into the application object
database, and reconciles base, local docs, and canonical docs.

- Up-to-date content changes nothing.
- A clean merge writes docs and creates one explicit sync commit authored with
  the user's Git identity: `[SANHO] Sync docs to <oid12>`.
- A conflict writes ordinary conflict markers and `sync.json`, then exits 0.
  Conflict is a successful sync outcome and must be read from output/JSON.

The user resolves conflicts with normal Git commands, commits any resolution
edits, and runs `sanho sync --continue`. Taking the unchanged local side remains
a valid resolution. Continue requires:

- a valid sync note;
- no remaining markers;
- a clean docs index and worktree;
- any resolution edits represented by committed `HEAD` content;
- HEAD on the sync's entry history or a descendant;
- a recorded merge tree whose non-conflicting paths remain unchanged in the
  committed resolution.

Differences on recorded conflict paths are the merge drift of an accepted
resolution. A difference anywhere else, or a legacy note with no recorded merge
tree, leaves the note and previous base untouched. Completion records the target
base and clears the note without creating another commit. `sanho sync --abort`
restores docs from current `HEAD`, restores the recorded previous base when it
can be corroborated (otherwise clears it), and clears the note. Existing commits
remain. A corrupt note still counts as active, so publication and mutation
remain blocked while abort stays available.

`sanho sync --rebase-onto <commit>` is the explicit recovery path after a
canonical history rewrite. The target must exist in the private clone and be a
meaningful replacement anchor. Sanho never guesses an unsafe target.

`sanho pull` is the consume-only path. It succeeds only when there is no local
docs work to preserve; otherwise it directs the user to `sanho sync`. `--commit`
creates an explicit user-authored commit.

Status reports two distinct local views. `sync_preview` predicts the merge of
the committed `HEAD` docs against the cached canonical state. `working_copy`
and `local_readiness` inspect the current docs index and worktree using the
same guard order as sync and pull. Readiness covers only locally provable
preconditions; it does not promise that a future fetch or network operation
will succeed.

`sanho diff` is the read-only inspection path. By default it compares the
recorded base tree with the cached canonical head; `--refresh` fetches first,
and `--local` instead compares the base with application `HEAD`. It disables
external diff drivers and text conversion, writes no application ref, index,
worktree, base, or registry state, and reports paths relative to the configured
docs root.

`sanho log` is the reader for the publication provenance recorded above. It
lists canonical commits newest first from the cached clone, bounded by
`--max-count` and optionally narrowed to one docs-root-relative `--path`, one
`--repository`, or one `--workspace`; `--refresh` fetches first. The source
filters are applied twice by design. Git narrows on the literal text the
publication commit convention writes, which is what keeps `--max-count`
bounding entries listed rather than entries examined; membership is then
decided on the decoded provenance, so a message carrying that text for a
reason of its own is not reported as a publication it is not. An `external`
commit matches no source filter, because it records none. Each entry is classified `publication` when its
message decodes to the canonical commit convention, or `external` for a commit
made directly in the canonical repository, whose `source` is null rather than
empty. It writes no application ref, index, worktree, base, or registry state,
opens no network without `--refresh`, and deliberately requires no recorded
base: it is the listing the rewrite-recovery guidance names, and no base
resolves in that state.

`sanho show` is the content reader for one canonical commit. It resolves the
revisions `sync --rebase-onto` accepts — a full or abbreviated OID, or a ref —
lists the documents that commit publishes, and with `--path` prints one of them
as of that commit. Binary content is classified rather than returned: the
marker contract's binary rule applies to inspection too, so a binary document
reports its size and produces no bytes on stdout, and text past the same scan
limit is refused instead of being loaded without bound. It writes no
application ref, index, worktree, base, or registry state, opens no network
without `--refresh`, and like `log` requires no recorded base — it exists for
the rewrite-recovery state, where an `external` candidate carries no provenance
and only its content can settle whether it is a safe anchor.

`sanho preview` is the publication counterpart of `status`'s sync preview. It
runs the publication evaluation pass above — steps 1 through 5 and the case
analysis — for one branch and stops before any commit or compare-and-swap, so
it changes no application ref, index, worktree, base, or registry state and no
canonical ref. It differs from the hook in two deliberate ways: it does not
fetch unless `--refresh` is given, because it is a reader and every reader
defaults to the cached snapshot, and it does not retry, because there is no
compare-and-swap race to lose without a push. A publication rejection is
returned as the verdict it is — `sync_in_progress`, `markers_present`,
`sync_required`, `history_rewritten`, or `empty_publication` — and the command
still exits 0, on `doctor`'s principle that a diagnostic which fails whenever
it finds a problem cannot be used to investigate one. Only a failure to reach a
verdict at all, such as an unreachable canonical, is an error. Preview names no
next command: every blocked verdict is a state the push rejection already words
with a proven recovery, and duplicating that guidance would duplicate the
catalog that closes it. A push of several branches at once is not previewed.

`sanho check` evaluates only policies the caller explicitly selects.
`--require-clean` checks the docs index and worktree, `--require-current`
fetches and requires the base to equal canonical head, and
`--require-published` requires committed docs to equal the recorded base.
Selected policies use AND semantics. An unmet policy exits 1 with a complete
result document; an inability to evaluate uses the normal command error and
JSON envelope. Check does not grant permission to commit or push.

## State and persistence

| Path | Mode | Contract |
|---|---:|---|
| `.sanho.json` | `0644` | v2 workspace config plus optional `hook_mode`/`hook_dir` |
| `.sanho_base.json` | `0644` | guarded base pointer |
| `.sanho_docs_hash` | preserved | read-only legacy base fallback |
| `.sanho_docs_hash.bak` | preserved | migration rollback copy, ignored by Git |
| `<git-dir>/sanho/sync.json` | `0644` | active conflicted-sync note |
| `<git-common-dir>/sanho/canonical` | `0700` | private bare clone |
| `~/.sanho/state.json` | `0600` | v2 registry |
| `~/.sanho/state.json.bak` | `0600` | byte-identical registry backup |
| `~/.sanho/state.lock` | `0600` | exclusive lock target |

`SANHO_HOME` must be absolute; otherwise `~/.sanho` is used. The directory is
forced to `0700`. State writes use the shared atomic writer and preserve the
specified modes.

`sanho state` reads a v2 registry normally. When it encounters the supported
legacy registry schema, it projects projects and workspaces into v2 structures
in memory. This compatibility read must not change either registry file.
Registry writers continue to reject the legacy schema until migration.

The registry is observational. Canonical publication correctness does not rely
on sibling entries. Sibling relationships may be `unknown` when one workspace's
private clone lacks another workspace's reported object.

One onboarding convenience reads the project table: `sanho init --project`
may reuse an existing non-empty docs repository URL. That value is copied into
the new workspace config before cloning; all later publication correctness
continues to depend on the workspace config, canonical Git state, and guarded
base rather than on the registry. An explicit conflicting URL is refused.

`sanho workspace forget <workspace-id>` removes exactly one observational row
whose recorded checkout path no longer exists. It never changes project
registrations, workspace files, Git state, hooks, or canonical clones. A live
path is refused so normal workspace cleanup remains responsible for removing
owned state.

## Concurrency

- Registry read/modify/write operations hold `state.lock`.
- Canonical merge helpers hold the shared clone lock.
- Publication is serialized across machines by remote compare-and-swap.
- State files are written atomically; partial primary files fall back to the
  backup where the contract permits it.
- Process cancellation terminates the Git process group and drains pipes for a
  bounded interval.

No local lock is claimed to coordinate different machines.

## Git execution policy

Every Git call goes through `internal/infra/gitx` with argv, never a shell
command line. Repository-scoping variables inherited from hooks are removed
before Sanho invokes Git for another repository. Network operations set
`GIT_TERMINAL_PROMPT=0` and an SSH command with `BatchMode=yes`.

No minimum Git version is enforced at startup. Merge paths require Git 2.38 or
newer in practice. Shared object reads require `--no-lazy-fetch`, and unborn
HEAD checks require `show-ref --exists`, as described under
[trustworthy reads](#trustworthy-head-and-object-reads). A capability failure is
reported at the operation that needs it. Exit codes that carry Git meaning are
read explicitly instead of being collapsed into generic failures.

## Git hooks

Sanho manages six hooks:

| Hook | Purpose | Failure policy |
|---|---|---|
| `pre-commit` | staged marker gate and local freshness | blocks markers only |
| `commit-msg` | provenance stamp | fail-open |
| `pre-push` | publication and push gates | fail-closed |
| `post-checkout` | base re-derivation | fail-open |
| `post-merge` | base re-derivation | fail-open |
| `post-rewrite` | base re-derivation | fail-open |

Default hooks call the canonical absolute path of the executable that performed
`init` or `migrate`. Commit/post hooks stand down if that executable disappears;
pre-push fails closed.

A repository-local custom `core.hooksPath`, including a recognized Husky 9
`.husky/_` layout, is managed only with `--manage-custom-hooks`. External,
global, unrecognized, or symlinked paths are rejected before workspace state is
written. Husky generated shims are validated but never modified; Sanho edits the
user scripts in `.husky/`.

Custom/Husky scripts use portable `sanho` lookup because they may be tracked.
Commit/post calls stand down when `sanho` is absent from PATH; pre-push remains
fail-closed. Foreign content and mode are preserved, existing failure status is
not hidden, and only exact recognized Sanho lines are added or removed.
Migration backups beside custom scripts are excluded through exact paths in the
Git common directory's `info/exclude`, never with a broad `*.bak` rule.

The approved hook mode and directory are persisted. If `core.hooksPath` later
changes, `doctor --fix` warns and modifies neither the recorded nor the new
target.

## Legacy workspace boundary

Legacy workspace detection is a current v0.2 safety boundary, not an operating
mode. Commit and HEAD-moved hooks print one migration hint and remain fail-open;
pre-push fails closed. `state` may inventory the legacy registry read-only,
`clean` remains available, and `migrate` is the only conversion command.
Detailed release history and compatibility changes live in `CHANGELOG.md`.

## User guidance and exit codes

Every user-facing next command is declared in the CLI message catalog. Unit
tests reject uncatalogued commands, and the E2E guidance-closure suite creates
the named state and executes each recommendation.

| Exit | Meaning |
|---:|---|
| 0 | success, including a sync that produced conflicts |
| 1 | actionable repository or environment state |
| 2 | internal defect |

`doctor` exits 0 when it reports warnings and exits 1 only when diagnosis itself
cannot run. Its base rows report two independent facts: whether local history
agrees with the recorded base, and whether canonical still contains it. The
second needs the fetched clone and is decided by reachability from the
publication branch, because a rewritten-away base object survives in the clone
and resolving it proves nothing.

A malformed invocation is classified at the CLI boundary, before any use case
runs: a rejected flag or a rejected positional argument carries
`invalid_arguments`, exits 1, and writes exactly one JSON envelope when the
command has a JSON document. The boundary owns that envelope precisely because
the command it was meant for never executed. A command name that resolves to
nothing exits 1 at every level of the command tree, not only at the root.

## Structured diagnostics and sync inspection

The CLI uses typed completion findings to expose error details and recovery IDs
from its guidance catalog. Read-only inspection uses the same assessment with
strict local Git readers; [CLI JSON](cli-json.md#sync-inspection) defines its
public interface. The [EPIC-003 roadmap](roadmap/README.md#epic-003-structured-diagnostics-and-sync-inspection)
records the feature's delivery and acceptance status.

### One completion assessment

`AssessCompletion` in `internal/usecase/docsync` supplies the completion
assessment used by `Continue` and inspection.
It requests facts through read-only ports and never performs completion state
writes. Its ordered checks are note existence/validity,
remaining markers, clean docs, entry-history ancestry, a recorded merge tree,
and preservation of paths outside the recorded conflict set. The first blocker
remains the command's refusal; subsequent checks are explicitly unevaluated.

Inspection binds strictly non-executing Git readers as described below;
Continue retains the existing filter-aware Git comparison behavior. They share
the decision algorithm, not permission to run the same subprocesses. When an
inspection reader cannot supply a fact safely, return a typed assessment error,
not a new completion blocker. For unchanged inputs that inspection can assess,
both surfaces must agree on the first blocker, path evidence, and drift count.
The absence of an inspection verdict does not prove Continue would be refused.

The assessment returns typed findings and evidence, not CLI strings. Its
observable facts include the note's entry/target/merge identities, current HEAD,
recorded conflict paths, and the allowed/unexpected path partition from the
existing tree comparison. A non-conflict change never becomes acceptable by
rewriting the conflict set. Preserve the whole-docs worktree marker scan above,
including markers outside recorded conflicts, and the existing treatment of
legacy entry records. An absent legacy merge tree still prevents completion.

`ResolutionState` is a reporting heuristic for hooks, not a completion proof.
In particular, a clean resolution that keeps the local side need not create a
new commit or change a conflict path. Do not gate inspection or Continue on the
heuristic's `resolved` label. Reuse overlapping helpers where safe without
silently changing the existing hook lifecycle or commit behavior.

Continue obtains a fresh assessment on each invocation and retains its guarded
mutation phase. Preserve the actual write order: clear the sync note first,
then write the corroborated target base, with the existing adapter-side guard.
This feature adds no transactional rollback or atomicity promise across those
writes. Inspection readiness describes local preconditions only; it is not a
promise that later persistence will succeed. Preserve the current failure
behavior when the note clear or base write fails, and verify it explicitly.

### Read-only boundary

Bind inspection using application and worktree-state read capabilities only.
Do not construct the normal sync use case through `docsyncUseCase` if doing so
would ensure or fetch a canonical clone. Do not read or update the registry
merely to inspect a sync. Use the worktree-specific Git directory for the note,
not the common directory's sibling state.

Inspection must not change docs, unrelated files, the real index, HEAD or any
application/canonical ref, Git operation metadata, hook files, configuration,
base files or backups, the sync note, or registry files. It must not create or
repair a clone, run hooks, fetch, probe a remote, push, commit, or clear state.
This prohibition includes writes by programs launched indirectly by Git, even
to paths outside the repository. A scratch index is not an execution sandbox.
`WorktreeDocsTree()` runs `git add -A`, and `DocsClean()` uses `git status`;
both require the inspection execution policy, not only the final comparison.

The initial execution policy is deliberately conservative:

1. Before a reached check uses status, worktree normalization, or scratch
   staging, read effective Git configuration without invoking configured
   programs. Include system, global, repository, worktree, include/includeIf,
   and command/environment configuration with Git's normal precedence.
2. If any effective `filter.<driver>.clean` or `filter.<driver>.process` has a
   non-empty command, stop before the filter-capable operation with the
   `inspection_unavailable` / `external_filter_configured` error. This first
   implementation gates the configuration, even when that driver might not
   apply to docs. Attribute-scoped exemptions are outside this Epic. Never run
   a filter to discover whether it is harmless, and never disable conversion
   and then describe raw-byte comparison as equivalent Git content.
3. Apply inspection-only runner controls before any affected Git invocation:
   disable optional index-refresh writes, hooks (including scratch-index
   change hooks), fsmonitor services, external diff and textconv, Trace2 file
   destinations, and automatic maintenance. Do not invoke smudge/process
   filters or checkout paths. Required objects must be read locally without
   lazy fetching. When these controls cannot be established, return
   `inspection_unavailable` / `execution_policy_unavailable`; do not run the
   uncontrolled command. Configuration read failures remain ordinary errors.
4. Reuse the current worktree-to-tree normalization only after that admission,
   with the same policy on its nested Git calls. Seed its disposable index
   from a verified HEAD, or empty only for a verified unborn HEAD. Retain Git's
   built-in attribute conversions, tracked-but-ignored paths, modes, symlinks,
   and exact path semantics. Do not substitute `HEAD`'s docs tree merely
   because status was clean; that shortcut is not part of this plan.

The strict adapter rejects an indexed docs gitlink before status or scratch
staging with `execution_policy_unavailable`. Git status can run a nested repository's
filters even with recursion disabled, and the parent configuration check
cannot establish that nested execution policy. Continue retains its ordinary
Git behavior for submodules. Scratch staging also rejects any new gitlink
created from an untracked embedded repository before returning a tree.

Keep the safety probe and affected invocations consistent. Check the effective
execution configuration again before another filter-capable operation, and
stop on a detected change instead of continuing under earlier admission.
Use command-scoped controls, not edits to user configuration or inherited
settings of unrelated commands. This is a bounded observation under ordinary
concurrent work, not an OS sandbox against a process actively replacing Git
or its configuration; do not advertise stronger isolation. Preserve existing
Continue filter support rather than applying the inspection restriction to it.

A no-note/corrupt-note result or a marker blocker reached before worktree
normalization still reports that earlier diagnosis; a later filter restriction
must not replace it. Suppressing fsmonitor is an execution control, not proof
of cleanliness, and the actual docs check must still run on admitted inputs.

Admitted comparison may create a disposable scratch index and bounded,
unreferenced local Git objects. Clean up disposable files on success, error,
and cancellation; do not run GC or delete shared objects as cleanup. This
allowance does not authorize external programs and does not promise a
byte-identical object database. No filesystem-wide traversal is introduced.

### Trustworthy HEAD and object reads

`HeadCommit()`, `HeadDocsTree()`, and the admitted scratch seed path verify
the references and objects they require. A nonzero Git exit
alone is not evidence of an unborn HEAD. Classify HEAD as unborn only when it
is a valid symbolic reference to an absent local branch. A valid detached HEAD
is an ordinary committed state. An existing ref whose object is missing, an
invalid HEAD/ref, an unreadable or corrupt object/index, and a Git execution
failure must propagate as errors. If absence itself cannot be established,
return an error rather than inventing an empty history.

Use Git-supported reference queries rather than assumptions about loose ref
files; packed refs and linked worktrees must work. Ref existence and commit
object validity are separate checks. Shared object readers require Git's
`--no-lazy-fetch` control, and unborn classification also requires
`show-ref --exists`. An unsupported capability is an execution error; it is
never evidence of absence. Preserve existing error
classification for surfaced failures; removing a success-shaped fallback is
an intentional correctness fix, not an incompatible change to valid inputs.

`seedScratchIndex()` may use `read-tree --empty` only after positive unborn
classification. Failure of `read-tree` for a verified commit must be returned.
Likewise, an empty docs tree is valid only for a verified unborn state or a
verified existing tree with no docs subtree. Missing required commit, tree, or
blob objects must not be mapped to an empty tree, zero drift, or a clean result.
These reads use local objects on both ordinary and inspection runners. Missing
docs objects in a partial clone remain errors; they do not trigger a fetch.
Ordinary worktree normalization still supports configured filters.
Do not repair, fetch, or rewrite refs while determining these facts. This is a
bounded repair of helpers used by the feature, not a repository-wide Git audit.

### Observation limits

The inspection is advisory, not a capability token. Detect HEAD or sync-note
changes across the assessment and invalidate the read as the CLI contract
specifies. Do not introduce a daemon, session, lease, persistent inspection
record, or second lock/scheduler framework to make the observation appear
atomic. Later mutations always recheck their own preconditions.

### Typed diagnoses and shared guidance

Keep domain/use-case blocker types independent of CLI wording and recovery
identifiers. A typed path-bearing cause must preserve `errors.Is` relationships
with the existing sentinels so error codes, hook handling, and exit behavior
remain stable for existing outcomes. The new inspection-only availability
error has its own code in the inspection CLI contract; it must not be mapped to
an existing completion refusal. Paths are data carried from Git results, not
comma-separated text recovered from an error message.

The CLI owns the mapping from typed causes to public reasons and recovery IDs.
Extend the existing `messages.go` guidance catalog rather than building a
parallel policy table in the skill, inspection command, or error renderer.
The human recovery sequence and machine recovery ID must resolve to the same
catalog definition; the closure suite proves the named prerequisites and
commands in the relevant states. A recovery ID does not authorize its effects.
In particular, abort and restart guidance must preserve the requirement to
review and protect user work before acting.

Keep unknown operational errors as errors. The assessment must not convert a
Git/I/O failure into a successful negative diagnosis or an empty conflict list.
A generic error code alone is insufficient to infer the more specific reason.
No new JSON output is added to hook entrypoints.

### Adoption and compatibility

There is no persistent-state schema change, registry migration, hook
installation, global skill upgrade, or release-version change in this Epic.
Existing managed workspaces and active notes are assessed in place. Missing
legacy information remains explicitly missing; inspection never repairs or
rewrites it. The regular status report remains the lightweight existing view;
full inspection is conditional, not an additional mandatory command at every
commit or push boundary.

## Related documentation

- [Operations](operations.md)
- [Recovery](recovery.md)
- [Deployment](deployment.md)
- [CLI JSON](cli-json.md)
- [Hands-on testing](hands-on-testing.md)
