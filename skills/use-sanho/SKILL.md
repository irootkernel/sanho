---
name: use-sanho
description: Use Sanho safely when preparing an authorized Git commit or push in a Sanho-managed workspace, handling a Sanho warning or rejection, or performing an explicitly requested Sanho initialization, synchronization, lifecycle, or recovery action. Do not use for routine editing, review, build, or test work that does not approach a commit or push boundary.
---

# Use Sanho

Sanho synchronizes an application's `docs/` directory with a canonical Git
repository. It is a Git-boundary tool, not a general task or session manager.

## Normal workflow

1. Confirm this request reaches a commit/push boundary or explicitly concerns
   Sanho. Otherwise, do not invoke Sanho.
2. If availability is not already established in the current environment, run
   `command -v sanho` and `sanho version --json`. If unavailable, report that
   fact; never install or upgrade it automatically.
3. At an authorized commit boundary, run `sanho status --json`. At an
   authorized push boundary, run `sanho status --refresh --json`. A
   `not_in_workspace` error means the current repository is not configured;
   do not initialize it without explicit user intent. Use
   `sanho state --all --json` only when project or workspace inventory matters.
   Use `sanho diff`, `sanho diff --refresh`, or `sanho diff --local` when the
   user asks to inspect incoming or unpublished docs changes. Diff is
   read-only, has no JSON mode, and prints paths relative to the configured
   docs root.
   Use `sanho log` (with `--refresh`, `-n`, `--path`, or `--json`) when the
   user asks what changed in canonical or which repository, workspace, or
   application commit a document came from. Log is read-only, needs no
   recorded base, and reads the last fetched snapshot unless `--refresh` is
   given. Read `kind` before `source`: an `external` entry is a commit made
   directly in the canonical repository and reports `source: null`, which is
   absent provenance rather than an empty record.
   Use `sanho check --require-clean`, `--require-current`, and/or
   `--require-published` when automation needs an explicit policy. Parse the
   complete result even when exit 1: `passed:false` is a policy mismatch,
   while an `error` envelope means evaluation failed. A passing check never
   grants commit or push permission.
4. Parse the JSON document and process exit separately. Branch on stable
   `error.code`, `known` fields, `sync_in_progress`, `relation`,
   `publication`, `sync_preview`, `working_copy`, and `local_readiness`.
   Treat `sync_preview` as a prediction for committed `HEAD` and readiness as
   current local preconditions; neither proves that a later fetch or network
   operation will succeed. Never infer unknown values as zero or a sync
   outcome from exit 0. Exit 2 means an internal Sanho defect; stop and report
   it.
5. Derive the next action from current output and Sanho's current guidance,
   not conversation memory. When a Sanho message names a next step, follow
   the message's own sequence, not one command out of it — some named
   commands require a preceding step the message states (resolve,
   `git add docs/`, `git commit`, then `sanho sync --continue`). Named
   commands this skill gates — abort, rewrite recovery, repair,
   initialization, migration, cleanup — still require explicit user
   intent. A pre-commit behind warning is non-blocking: the commit has already
   succeeded, so do not retry it or automatically run the mutating sync. When
   current status requires reconciliation before an authorized push, run
   `sanho sync` within that authorization; after an authorized push rejection,
   perform its named recovery sequence and retry the same push. When Sanho
   states manual intervention is required, stop and follow
   [recovery.md](references/recovery.md). Use preconditions and idempotent
   operations where the CLI supports them. Let Git run Sanho's hooks; never
   use `--no-verify`, force operations to evade a guard, or manual edits to
   Sanho-managed state.
6. Perform the authorized Git or Sanho mutation before recording it as done.
   Re-run the appropriate status command after every mutation and report only
   claims supported by that evidence. Sanho never grants commit or push
   permission.

Canonical publication happens through an authorized `git push`. Sanho has no
`sanho push` command, and a successful local commit is not publication.

Routine progression may change docs and create commits: a clean `sanho sync`
that changes docs writes a `[SANHO] Sync docs to <oid>` commit with the user's
Git identity, and `sanho pull --commit` creates the same kind of commit when
the pulled docs differ from HEAD's. Keep those actions within the user's
existing mutation and Git authorization. Require explicit intent for
initialization, workspace replacement or removal, abort, rewrite recovery,
migration, project changes, or repair.

## Specialized workflows

- Read [lifecycle.md](references/lifecycle.md) only for installation
  diagnostics, initialization, migration, cleanup, project changes, or other
  lifecycle requests.
- Read [authoring.md](references/authoring.md) only for built-in configuration
  and custom hook selection.
- Read [recovery.md](references/recovery.md) only for stale state, interrupted
  or uncertain mutations, active syncs, rewrites, locks, or network failures.

Sanho has no session/task runtime, daemon or service, cancellation/reset/goal
commands, durable job queue, or custom workflow/policy/manifest authoring.
Never invent commands or imply those capabilities.
