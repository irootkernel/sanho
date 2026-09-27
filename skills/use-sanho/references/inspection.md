# Sanho Inspection

Read the section that answers the request. For commands that support
`--refresh`, that flag fetches canonical without publishing; omitting it reads
the last fetched snapshot, which must be reported as cached. The current-state
policy check below fetches automatically.

## Inspect sync completion

For an active-sync diagnosis or uncertainty about completion, run
`sanho sync --inspect --json` when supported. It reads local application state
without fetching or requiring the canonical clone. It is optional diagnostic
work, not another step in every successful commit or push.

Read `state` first: `none` means no sync note; `corrupt` means a present note is
unusable; `active` reports its completion assessment; `changed` invalidates an
observation after a detected HEAD or note change. Discard a changed snapshot
and obtain a fresh read only if needed; do not retry indefinitely.

Use `continuation.ready`, `reason`, exact `paths`, and `recovery_id` to select
the relevant [recovery](recovery.md#complete-an-active-sync). A blocked diagnosis
still exits 0. Checks after the first blocker are `not_evaluated`, and accepted
legacy entry history may be `not_applicable`; neither proves a check passed.
`comparison` is null until evaluated, then separates allowed conflict-path
changes from unexpected changes. The note's target is not an adopted base.
Keep paths as array values, including embedded commas, newlines, and Unicode.

A ready result is a bounded local observation. It grants no mutation authority,
does not guarantee persistence or publication, and is not a token for Continue.
Continue always reassesses current state. Reconcile already completed steps
before any authorized mutation, including a resolution commit.

`inspection_unavailable` exits 1 with an error envelope and no readiness
verdict. `external_filter_configured` conservatively covers any effective
non-empty clean/process command, including unused drivers and global settings.
`execution_policy_unavailable` means the required reader controls could not be
established or their admission changed. Earlier no-note, corrupt-note, or
marker diagnoses can still finish before a filter-capable read is needed.
Stop and report either availability limitation. Do not change filters, retry
with ordinary status/sync, or invoke Continue to bypass it. Continue retains
filter-aware normalization, but this fact does not authorize that fallback.
Other Git/I/O errors remain errors, not empty or ready evidence.

If an older installed binary rejects the `--inspect` flag, report that
capability limit and use its existing supported read-only evidence, such as
local status and Git state, without claiming the new assessment. Never remove
the flag and run mutating `sanho sync`. Unsupported syntax and
`inspection_unavailable` are different cases; the compatibility fallback does
not override the execution-policy stop above. Do not install or upgrade merely
to make this diagnostic available.

## Diff incoming or unpublished changes

Use `sanho diff` for incoming changes, `sanho diff --refresh` for a fresh
canonical comparison, and `sanho diff --local` for unpublished local docs.
`--stat` and `--name-only` narrow the output. Diff has no JSON mode and prints
paths relative to the configured docs root. It requires a recorded base.

## Read history and provenance

Use `sanho log` with `--json` when the question is what changed in canonical or
which application repository, workspace, or commit published a document.
`--refresh` fetches first; `-n` bounds the listing and `--path` narrows it to a
docs-root-relative path. Log needs no recorded base.

Read `kind` before `source`: an `external` entry was committed directly in
canonical and has `source: null`. That is absent provenance, not an empty
publication record. Do not attribute it to an application repository.

Use `--repository` or `--workspace` to select one publication source. Take the
exact non-empty value from a listing's `source` fields or the relevant
`sanho state --all --json` inventory; empty filters return `invalid_arguments`.
Multiple filters use AND semantics. Source filters exclude `external` commits.
A filtered listing may be shorter than `-n` even when more matches exist deeper
in history, so its length does not establish exhaustion.

## Read one canonical commit

Use `sanho show <commit>` with `--json` to list the documents at a canonical
commit; add `--path <document>` to read one. `--refresh` fetches before reading.
Show needs no recorded base and accepts the revisions that
`sanho sync --rebase-onto` accepts. This makes it useful when inspecting a
rewrite-recovery candidate, especially an `external` commit with no provenance.
A history path filter says which commits touched a file, not what a candidate
contains.

`document.content` is null exactly when `document.binary` is true. This is the
complete binary result, not truncated text; do not retry it as a failed read.

## Preview a push

Use `sanho preview --json` when asked what a push would do, or when its
publication verdict would help choose the next authorized step. It evaluates
publication without writing a canonical commit, refs, or workspace state.
Use `--refresh` when the verdict must be current: the pre-push hook always
fetches, so a cached preview does not predict its later snapshot. Read
`canonical.data_age_seconds` when describing cached evidence.

Branch on `blocked` and `verdict`, not the process exit. A blocked push is a
verdict at exit 0; only failure to reach a verdict produces an error envelope.
`publishes` distinguishes a publication from an up-to-date no-op. `--branch`
selects another local branch; preview does not model a multi-branch push.
A preview never grants permission to push.

## Apply an explicit policy

Use `sanho check --json` with at least one of `--require-clean`,
`--require-current`, or `--require-published` when automation needs that policy.
`--require-current` fetches canonical before evaluation. Selected checks use
AND semantics. Parse the complete result even at exit 1:
`passed: false` means a policy mismatch; an `error` envelope means evaluation
failed. A passing check does not authorize a commit or push.
