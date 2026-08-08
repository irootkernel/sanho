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

Marker detection is scoped to the relevant diff:

- pre-commit scans staged changed docs;
- pre-push scans docs changed by pushed commits;
- sync completion scans the paths in the active conflict set.

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

The user resolves conflicts with normal Git commands, commits the resolution,
and runs `sanho sync --continue`. Continue requires:

- a valid sync note;
- no remaining markers;
- a clean docs index and worktree;
- committed resolution work;
- HEAD on the sync's entry history or a descendant;
- an absorption proof that the resolution did not silently discard unrelated
  canonical content.

Completion records the target base, reports merge drift, and clears the note. It
does not create another commit. `sanho sync --abort` restores the recorded
pre-sync docs/base and clears the note. A corrupt note still counts as active,
so publication and mutation remain blocked while abort stays available.

`sanho sync --rebase-onto <commit>` is the explicit recovery path after a
canonical history rewrite. The target must exist in the private clone and be a
meaningful replacement anchor. Sanho never guesses an unsafe target.

`sanho pull` is the consume-only path. It succeeds only when there is no local
docs work to preserve; otherwise it directs the user to `sanho sync`. `--commit`
creates an explicit user-authored commit.

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
newer in practice; a capability failure is reported at the operation that needs
it. Exit codes that carry Git meaning are read explicitly instead of being
collapsed into generic failures.

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
cannot run.

## Related documentation

- [Operations](operations.md)
- [Recovery](recovery.md)
- [Deployment](deployment.md)
- [CLI JSON](cli-json.md)
- [Hands-on testing](hands-on-testing.md)
