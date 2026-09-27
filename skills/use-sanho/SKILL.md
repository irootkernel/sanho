---
name: use-sanho
description: Use Sanho for authorized commits or pushes in Sanho-managed workspaces, Sanho warnings or rejections, and explicit Sanho operations. Do not use for routine editing, review, build, or test work.
---

# Use Sanho

Sanho synchronizes an application's docs with a canonical Git repository.
Activate this skill only at the Git boundaries or for the requests above.

## Commit and push

1. Reuse verified availability and workspace configuration while the environment
   is unchanged. If availability is unknown, run `command -v sanho` and
   `sanho version --json`; report absence without installing or upgrading it.
   A `not_in_workspace` error means the repository is not configured. It grants
   no initialization authority. Do not run Sanho for subsequent unrelated work.
2. Before an authorized commit, read current Git state and run
   `sanho status --json`. Keep this check local; a cached canonical snapshot
   does not establish current remote state. Perform the authorized Git commit
   and determine its result from Git's exit status, resulting HEAD, and relevant
   index/worktree state. A pre-commit freshness warning proves neither success
   nor failure. Never repeat a successful commit or run sync merely because of
   that warning. Reconcile an uncertain result through
   [recovery](references/recovery.md#reconcile-the-affected-state) before retrying.
3. Before an authorized push, run `sanho status --refresh --json`. Its canonical
   fetch is covered by that push's authorization; it needs no separate network
   approval. If current state requires reconciliation, run `sanho sync` within
   the existing authorization for that target and its effects, including any
   commits it creates. If Sanho's pre-push hook rejects, follow its complete
   CLI-advised recovery sequence. If the application
   remote rejects after Sanho published docs, check both remote outcomes before
   retrying; the docs publication and local commits remain in place. For a
   partial multi-ref result, check that retrying an outstanding ref preserves
   current canonical docs. Do not ask again for effects already covered;
   obtain authority if the target or effects exceed that scope.
4. For an active-sync diagnosis or uncertain completion, use
   [sync inspection](references/inspection.md#inspect-sync-completion) when the
   installed binary supports it. Do not add inspection to every commit or push.
   On `inspection_unavailable`, stop and report the limitation; do not retry
   through ordinary status/sync, change filters, or invoke Continue as a bypass.
   When inspection is available or no inspection is needed, follow Sanho's
   current output in order: resolve, stage, commit, then `sanho sync --continue`.
   For an unsupported flag, use only the compatibility diagnostics in the
   inspection reference. Load
   [recovery](references/recovery.md) for the failure being handled. When Sanho
   requires manual intervention, establish the recovery decision before acting.
   An explicit preview request or uncertainty about the publication verdict can
   justify [preview](references/inspection.md#preview-a-push); it is not a
   mandatory step for every push.
5. After each Git or Sanho mutation, re-read the relevant Git state and Sanho
   status: local status for a commit or local change, refreshed status when
   verifying canonical publication. Report only the result those checks prove.
   A local commit is not publication; publication happens through an authorized
   `git push`. There is no `sanho push` command.

## Evidence and authorization

Parse JSON and process exit separately. Use `error.reason`, exact `error.paths`,
and `error.recovery_id` when present; never split a message to recover paths.
An older binary may return only `code` and `message`: retain its existing
code-based recovery and current human guidance without inventing a typed reason.
A recovery ID identifies guidance, not an executable command or permission.
The source skill does not establish which features the installed binary has.

Branch on stable `error.code`, `known` fields, `sync_in_progress`, `relation`,
`publication`, `sync_preview`, `working_copy`, and `local_readiness`. Unknown
is not zero; `sync_preview`
predicts a merge of committed `HEAD`, while readiness describes current local
preconditions. Neither guarantees a later fetch or network operation. A sync
can exit 0 with `status: conflicts`; inspect the result before claiming success.
`invalid_arguments` means the invocation never ran: correct its arguments,
not the workspace. Exit 2 reports an internal Sanho defect; stop and report it.

Status, preview, and policy checks never authorize mutations. A clean sync that
changes docs creates a `[SANHO] Sync docs to <oid>` commit with the user's Git
identity; `sanho pull --commit` can create the same kind of commit. Keep both
within the user's mutation and Git authorization. Require explicit intent for
initialization, workspace replacement or removal, abort, rewrite recovery,
migration, project changes, or repair. Preserve unrelated work and any existing
Git operation. Honor command preconditions and supported idempotency without
assuming an uncertain mutation is safe to repeat. Let Git run installed hooks;
never bypass them with `--no-verify`, force operations that evade a guard, or
manual edits to Sanho-managed state.

## Read only the reference needed

- [Inspection](references/inspection.md): local sync completion, diff, history,
  provenance, commit contents, push preview, or explicit policy checks. Read the
  relevant section.
- [Recovery](references/recovery.md): stale state, interrupted or uncertain
  mutations, active syncs, rewrites, locks, or network failures.
- [Lifecycle](references/lifecycle.md): installation diagnostics, initialization,
  migration, cleanup, project or workspace changes.
- [Configuration](references/authoring.md): built-in configuration and custom
  hook selection.

Use `sanho state --all --json` only when project or workspace inventory matters.
Sanho has no session/task runtime, daemon, service, durable job queue, or custom
workflow authoring. Do not invent cancellation, reset, or goal commands.
