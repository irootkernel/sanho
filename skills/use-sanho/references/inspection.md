# Sanho Inspection

Read the section that answers the request. For commands that support
`--refresh`, that flag fetches canonical without publishing; omitting it reads
the last fetched snapshot, which must be reported as cached. The current-state
policy check below fetches automatically.

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
