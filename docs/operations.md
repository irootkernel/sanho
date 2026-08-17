# Sanho Operations Guide

This guide covers daily v0.2 operation and failure response. See
[Deployment](deployment.md) for installation and removal,
[Architecture](architecture.md) for contracts, and [Recovery](recovery.md) for
state-changing recovery procedures.

## Build and run

```bash
make cli-build       # bin/sanho
make cli-install     # install to Go's binary directory
```

Install a published release with an explicit version:

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.6
sanho version
```

Sanho has one executable and no service to start. Git and canonical-repository
credentials are the only runtime dependencies.

## Daily workflow

An already registered project can onboard another checkout without repeating
its canonical URL: `sanho init --project <name>`. Unregistered projects still
require `--docs-repo-url`, and an explicit conflicting URL is rejected.

### 1. Inspect

```bash
sanho status
sanho status --json
sanho status --refresh
```

The default uses the last fetched canonical snapshot and reports its age.
`--refresh` performs a network fetch first. Read three independent axes:

- `relation`: whether the base is behind or ahead of canonical;
- `publication`: whether local committed docs are pending publication;
- `sync_preview`: whether a sync is expected to be clean;
- `working_copy`: whether docs have staged, unstaged, or untracked changes;
- `local_readiness`: whether sync and pull pass their current local guards.

Unknown is not zero. A missing object or base produces `known:false` rather than
an invented relationship.

The sync preview describes committed `HEAD`, while readiness describes the
current index and worktree. A clean preview can coexist with `docs_dirty`: the
predicted merge is clean, but sync first requires the user to commit or stash
local docs work. Readiness does not test network availability or predict what
a later fetch will discover.

Inspect incoming changes before syncing:

```bash
sanho diff
sanho diff --refresh
sanho diff --stat
sanho diff --name-only
```

The default uses cached canonical state. `--refresh` fetches first. To inspect
committed docs waiting to be published, compare the recorded base with
application `HEAD`:

```bash
sanho diff --local
```

Diff paths are relative to the configured docs root. `--stat` and
`--name-only` are mutually exclusive, and `--local` cannot be combined with
`--refresh` because local comparison does not consult canonical origin.

Inspect who published what, and from where:

```bash
sanho log
sanho log --refresh
sanho log -n 50
sanho log --path api.md
sanho log --repository payments
sanho log --workspace product:/Users/me/work/app
sanho log --json
```

Each publication reports the source repository, branch, workspace, and
application commit it came from. A commit made directly in the canonical
repository is listed as ordinary history with no source. Paths follow the same
docs-root-relative convention as `diff`. Unlike `diff`, `log` needs no recorded
base.

`--repository` and `--workspace` answer "what did this repository send" in a
docs repository several repositories publish into. Take the exact values from a
listing's own `source` fields, or from `sanho state --all` for a workspace ID.
Because only publications record a source, neither filter ever lists a commit
made directly in the canonical repository, and an empty result says which
narrowing matched nothing rather than claiming canonical is empty.

Read a commit the listing named:

```bash
sanho show <commit>
sanho show <commit> --path api.md
sanho show <commit> --refresh --json
```

Without `--path` it lists the documents that commit publishes; with `--path` it
prints one of them as of that commit. The revision is anything the canonical
clone resolves — a full or abbreviated OID, or a ref — which is the same set
`sync --rebase-onto` accepts. A binary document reports its size on stderr and
writes nothing to stdout, so redirecting a text document to a file stays exact.
Like `log`, `show` needs no recorded base and reads the cached snapshot unless
`--refresh` is given.

### 2. Commit normally

Edit and commit docs with ordinary Git commands:

```bash
$EDITOR docs/api.md
git add docs/api.md
git commit -m "docs: update API"
```

Commit hooks are local and never fetch. A stale base prints one warning and the
commit succeeds. Complete staged conflict markers are the only docs condition
that blocks the commit.

### 3. Reconcile when behind

```bash
sanho sync
```

An up-to-date sync changes nothing. A clean merge creates one explicit commit
with the user's Git identity named `[SANHO] Sync docs to <oid12>`.

When sync reports conflicts it still exits 0: markers and an active sync note
were created successfully. Resolve with normal Git commands, then complete the
window explicitly:

```bash
$EDITOR docs/api.md
git add docs/
git commit -m "docs: resolve canonical sync"
sanho sync --continue
```

Do not infer this result from exit code alone. With `--json`, read `status` and
`conflicts`.

To discard the entire conflicted sync and restore its entry state:

```bash
sanho sync --abort
```

Abort is destructive and must be explicitly authorized.

### 4. Publish with Git

```bash
git push
```

There is no `sanho push`. The pre-push hook publishes docs to canonical before
Git updates the application remote. It either succeeds completely or rejects
the application push before any application remote ref changes.

Expected outcomes include:

```text
sanho: published docs <oid12> (fast_forward)
sanho: published docs <oid12> (merged)
sanho: docs already up to date
```

Tag-only and branch-deletion pushes pass through without publication analysis.
Sanho never moves application refs itself.

### 5. Consume without local docs work

```bash
sanho pull
sanho pull --commit
```

Pull is the fast-forward consume path. If local docs must be preserved, it
refuses and names `sanho sync`. Without `--commit`, pulled changes remain for the
user to review and commit.

### 6. Inspect all workspaces

```bash
sanho state
sanho state --all
sanho state --all --json
```

The registry is observational. It reports sibling bases but does not coordinate
publication. A sibling relationship may be unknown when this workspace's clone
does not contain the sibling's reported object.

If a checkout was deleted before `sanho clean`, remove only its stale
observation by copying its exact ID from `sanho state --all`:

```bash
sanho workspace forget <workspace-id>
```

The command refuses while the recorded path still exists. Run `sanho clean`
inside a live checkout instead so its hooks, clone ownership, and local state
are handled safely.

## Failure response

### Canonical unreachable

Read-only status can use cached data and says how old it is. Network write paths
fail closed:

```text
sanho: canonical repository unreachable (...)
Check network access to the docs repository, then push again.
error: push rejected — no remote ref was changed
```

Check credentials without enabling prompts, restore connectivity, and retry the
same command. Do not delete managed state or bypass the hook.

### Sync is active

```bash
sanho status
git status
```

Finish with `sanho sync --continue` after resolving and committing, or use an
authorized `sanho sync --abort`. Other docs-mutating commands and publication
remain blocked while the note exists. A corrupt note is still active.

### Canonical history was rewritten

Refresh status and follow the exact candidate command Sanho prints:

```bash
sanho status --refresh
sanho sync --rebase-onto refs/remotes/origin/main
```

The actual candidate may be a commit or the remote branch. When the printed
guidance names no candidate, list them with `sanho log` and read the one you
are considering with `sanho show <commit>` before adopting it. Resolve any
conflict normally and finish with `sanho sync --continue`. Never force-push
merely to make the old base reachable again.

### Base missing or corrupt

```bash
sanho doctor
sanho doctor --fix
```

Doctor re-derives a base only when history and current docs corroborate it. If
no safe base can be proved, run `sanho sync`; do not write `.sanho_base.json`
manually.

### Registry lock or corruption

The lock wait is bounded. Find the process holding `~/.sanho/state.lock` before
retrying; never delete the lock file as a bypass.

If the primary registry is corrupt, preserve both files before inspection.
Sanho may recover from `state.json.bak` where supported. If both are corrupt,
follow [Recovery](recovery.md#registry-recovery).

### Hook mismatch

```bash
sanho doctor
sanho doctor --fix
```

Doctor repairs the approved hook target. For managed custom/Husky hooks it
refuses to modify anything when the current `core.hooksPath` differs from the
recorded target.

## Diagnostics

`sanho doctor` checks Git, config, hooks, private clone, canonical reachability,
base, registry, sync state, and docs measurements. Warnings do not change its
exit code; inability to diagnose does.

Use `--verbose` for bounded Git diagnostics on stderr. With `--json`, stdout
remains one machine document.

## Policy checks

CI and agents can select the exact state they require:

```bash
sanho check --require-clean
sanho check --require-current --require-published --json
```

At least one condition is required and multiple conditions use AND semantics.
`--require-current` refreshes canonical automatically. An empty canonical with
no recorded base satisfies current because there is no upstream state to
consume. Policy mismatch exits 1 with every selected result; execution errors
use the normal error path. A passing check is evidence about workspace state,
not authorization to commit or push.

## Verification

Repository-standard gates are:

```bash
make docs-check
make test-prepare
make test-unit
make test-int
make test-e2e
make test
```

`make test` runs all required phases. The optional scale profile is separate:

```bash
SANHO_SCALE=1 make test-scale
```

Real hosting, SSH, filesystem, and installed-binary boundaries are checked with
the [hands-on checklist](hands-on-testing.md) after the automatic suite passes.
