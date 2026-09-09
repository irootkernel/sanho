# Sanho Recovery

Load this reference for the failure being handled. Preserve existing Git and
Sanho authorization for the same target and effects; recovery does not expand
it. Use the smallest supported action without weakening a safety fence.

## Reconcile the affected state

Use the original command's output and current evidence to select diagnostics:

- For a failed or uncertain commit, inspect Git's result, `git status
  --porcelain=v1`, `git rev-parse HEAD`, and the relevant commit or index diff.
  A freshness warning alone says nothing about whether Git created a commit.
- For sync progress or local readiness, run `sanho status --json` and inspect
  the relevant docs changes. Refresh only when the decision needs current
  canonical state.
- For a changed checkout, branch, or publication target, recheck the affected
  Git configuration and refs. Do not repeat known environment inventory when
  it cannot explain the failure.
- For missing or corrupt base, clone, or hooks, run `sanho doctor --json`.
  Doctor is not a required diagnostic for every warning or network failure.

Parse JSON error codes and exits separately. After an interrupted or timed-out
mutation, establish its actual outcome before retrying. If current evidence
cannot settle it, report the uncertainty rather than repeating the mutation.

## Complete an active sync

`sync_in_progress: true` means an unfinished sync window exists. Markers may
remain, or the resolution may already be committed. Follow the CLI's complete
sequence: resolve the affected docs, stage them, commit the resolution, then
run `sanho sync --continue --json`. Inspect current state and omit steps already
completed; do not create a duplicate resolution commit. Keep resolution edits
and commits within the existing authorization.

A plain sync can exit 0 with `status: conflicts`. Continue refuses unresolved
markers or an uncommitted resolution, reports `completed` on success, and may
report merge drift. Review that count; it is not an automatic failure.
Re-read local status after completion.

To discard the whole active sync, require explicit abort intent, preserve
unrelated work, and run `sanho sync --abort --json`. Abort is designed to be
idempotent after interruption; that does not authorize the initial destructive
decision. Reconcile its result before any retry.

## Reconcile stale or rewritten canonical history

Use `sanho status --refresh --json` when a stale canonical snapshot affects the
next step. Ordinary reconciliation for an authorized push can continue without
another approval when its target and effects, including sync commits, remain
covered.

For `history_rewritten`, require explicit rewrite-recovery intent. If Sanho
names a rebase target, use that exact value. If no target is named, list
candidates with `sanho log --refresh --json` and inspect them with
`sanho show <candidate-commit> --json`; let the user choose the anchor before
`sanho sync --rebase-onto <chosen-commit> --json`. Read the relevant
[history and commit inspection sections](inspection.md#read-history-and-provenance)
for provenance, filtering, and binary content. Neither command needs a base.

Never guess an anchor or force-push to recreate old history. If rebase recovery
conflicts, follow its full resolve, stage, commit, and continue sequence.
For diagnosed managed-state damage, use `sanho doctor --fix` only with explicit
repair authorization, then diagnose again and re-read status. It is designed
to be non-destructive but remains a repair decision.

## Network, locks, and uncertain publication

For `canonical_unreachable`, restore connectivity within the authorized scope
and refresh state. Retry only after establishing the original operation's
outcome and current preconditions. Do not delete the private clone or bypass
pre-push.
If a push result is uncertain, compare current application and canonical refs
and publication evidence before deciding whether another push is needed;
old stderr is not proof of the current result.

For `registry_lock_timeout`, identify the process holding the lock and let it
finish. Terminate it only when authorized and safe. Never delete lock or registry
files to bypass a live owner.

Sanho has no durable job queue or service to restart. Recovery reconciles state
and then performs the supported CLI or Git action; it does not invent session,
cancellation, or reset operations.
