# Sanho Recovery

Load this reference for the failure being handled. Preserve existing Git and
Sanho authorization for the same target and effects; recovery does not expand
it. Use the smallest supported action without weakening a safety fence.

## Reconcile the affected state

Use the original command's output and current evidence to select diagnostics:

- For a failed or uncertain commit, inspect Git's result, `git status
  --porcelain=v1`, `git rev-parse HEAD`, and the relevant commit or index diff.
  A freshness warning alone says nothing about whether Git created a commit.
- For an active sync or uncertain completion, use
  [sync inspection](inspection.md#inspect-sync-completion) where supported.
  Honor its `inspection_unavailable` stop before choosing another diagnostic.
  For other local readiness questions, use `sanho status --json` and the
  relevant docs changes. Refresh only when current canonical state is needed.
- For a changed checkout, branch, or publication target, recheck the affected
  Git configuration and refs. Do not repeat known environment inventory when
  it cannot explain the failure.
- For missing or corrupt base, clone, or hooks, run `sanho doctor --json`.
  Doctor is not a required diagnostic for every warning or network failure.

Parse JSON error codes and exits separately. When present, use typed `reason`,
lossless `paths`, and `recovery_id` rather than parsing English. Keep the
existing code-based handling and current CLI guidance for older two-field
envelopes; a generic code alone does not establish a new specific reason.
After an interrupted or timed-out mutation, establish its actual outcome
before retrying. If current evidence cannot settle it, report the uncertainty
rather than repeating the mutation.

## Complete an active sync

`sync_in_progress: true` means an unfinished sync window exists. Markers may
remain, or the resolution may already be committed. An `active_sync` refusal
can be clarified by conditional inspection. Use its `continuation.reason`, or
the actual refusal's `error.reason`, when available:

- `markers_remaining` or `resolution_uncommitted`: finish only the remaining
  resolution steps below, using the exact returned paths where supplied.
- `foreign_entry_history`, `non_conflict_paths_changed`, or `missing_merge_tree`:
  preserve committed work and follow the refusal-specific abort/restart
  guidance only with explicit abort intent. Repeating Continue or making an
  extra resolution commit cannot establish the missing proof.
- `sync_note_corrupt` or `invalid_sync_target`: report the unusable note and
  follow the supported CLI recovery with explicit authority. Never edit or
  delete managed state by hand.
- `no_sync`: there is no active completion to perform. Reconcile an uncertain
  earlier result rather than starting another sync merely to clear this reason.

For a usable active window, follow the CLI's complete sequence: resolve the
affected docs, stage them, commit the resolution, then run
`sanho sync --continue --json`. Inspect current state and omit steps already
completed; do not create a duplicate resolution commit. Keep resolution edits
and commits within the existing authorization.

A plain sync can exit 0 with `status: conflicts`. If continue refuses remaining
markers or uncommitted resolution edits, finish the named steps before retrying.
If HEAD is outside the sync's entry history or continue cannot verify the merge's
non-conflicting paths, follow its abort-then-sync guidance after obtaining
explicit abort intent; another resolution commit or repeated continue will not
repair that state. Continue reports `completed` on success and may report drift
on conflict paths. Review that count; it is not an automatic failure. Re-read
local status after completion.

To discard the whole active sync, require explicit abort intent, preserve
unrelated work, and run `sanho sync --abort --json`. Abort is designed to be
idempotent after interruption; that does not authorize the initial destructive
decision. It restores tracked docs from current HEAD, preserves existing
commits, restores or clears the previous base, and clears the sync note.
Reconcile its result before any retry.

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
Sanho's pre-push can publish docs before Git updates application remote refs.
If that remote then rejects one or more ref updates, the canonical publication
remains. Other refs in a non-atomic push may have advanced even when Git exits
nonzero. Check the per-ref Git result, current application refs, and refreshed
canonical publication before choosing a remedy.
Do not repeat a local commit or sync for work that already completed. A partial
multi-ref failure needs an additional docs check: retrying a rejected ref alone
can republish its older docs tree and remove content from an accepted ref. Before
retrying outstanding updates, verify their proposed publication preserves the
current canonical content that must remain. Reconcile any tip that would discard
it, with authorization for any new edits or sync effects. Retry only when the
remaining targets, effects, and current preconditions are covered by the
existing authorization.
If a push result is uncertain, compare current application and canonical refs
and publication evidence before deciding whether another push is needed;
old stderr is not proof of the current result.

For `registry_lock_timeout`, identify the process holding the lock and let it
finish. Terminate it only when authorized and safe. Never delete lock or registry
files to bypass a live owner.

Sanho has no durable job queue or service to restart. Recovery reconciles state
and then performs the supported CLI or Git action; it does not invent session,
cancellation, or reset operations.
