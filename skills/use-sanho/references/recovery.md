# Sanho Recovery

Load this reference only when state is stale, a mutation was interrupted or
has an unknown outcome, a sync is active, or a Sanho failure needs recovery.
Use the smallest supported action and never weaken a safety fence.

## Reconcile evidence first

Before retrying or repairing, inspect current Git and Sanho state:

```bash
git status --porcelain=v1
git branch --show-current
git rev-parse HEAD
git remote -v
sanho status --json
sanho doctor --json
```

Use `sanho status --refresh --json` only when current canonical state is needed.
Record exit codes and parse stable JSON error codes separately. An interrupted
or timed-out mutation has an unknown outcome until current state proves what
happened; do not blindly retry it or claim completion from old output.

## Active or uncertain sync

`sync_in_progress: true` means an unfinished sync window exists — conflict
markers may remain, or the docs may already be resolved and committed without
the sync being completed. After the user resolves, stages, and commits the
docs, normal completion is:

```bash
sanho sync --continue --json
```

A plain `sanho sync` exits 0 even when its JSON `status` is `conflicts`, so
read `status`, never the exit code. `--continue` refuses to run while markers
remain or the resolution is uncommitted, reports `completed` on success, and
may report merge drift — review the stated count; drift is not an automatic
failure. Re-read `sanho status --json` after the command.

To discard the whole active sync, require explicit user intent, preserve
unrelated work, and run:

```bash
sanho sync --abort --json
```

Abort is designed to be idempotent after interruption, but that does not make
the initial destructive decision implicit.

## Stale canonical state or rewritten history

Refresh first:

```bash
sanho status --refresh --json
```

For `history_rewritten`, require explicit recovery intent before any rebase
and take the target from the message that raised it. When the message names a
rebase target, use exactly that value. When it names none — the push rejection
states manual intervention is required and prints a candidate-listing
`git log` command; the sync message says only to pick a canonical commit —
inspect canonical history in the private clone and let the user choose the
target for:

```bash
sanho sync --rebase-onto <chosen-commit> --json
```

Never guess an anchor or force-push to recreate the old history. For a missing
or corrupt base or hooks, diagnose with `sanho doctor --json`. `doctor --fix`
reinstalls managed state and is designed to be non-destructive, but treat
repair as the user's decision: use it only with explicit repair authorization,
then diagnose again.

## Network, locks, and publication uncertainty

For `canonical_unreachable`, restore connectivity and refresh state before
retrying the original operation. Do not delete the private clone or bypass the
pre-push hook. If a push outcome is uncertain, compare current application and
canonical evidence before another push; old stderr is not current proof.

For `registry_lock_timeout`, identify the process holding the lock and let it
finish or terminate it safely. Never delete lock or registry files to bypass a
live owner.

Sanho has no durable job queue or service to reconcile or restart. Recovery is
state reconciliation followed by the smallest supported CLI or Git action. If
current evidence still cannot establish the mutation outcome, stop and report
the uncertainty rather than retrying destructively.
