# CLI JSON Output

Sanho exposes stable machine-readable documents for:

```bash
sanho version --json
sanho version --verbose --json
sanho status --json
sanho state --json
sanho state --all --json
sanho log --json
sanho show <commit> --json
sanho preview --json
sanho check --require-<policy> --json
sanho sync --json
sanho sync --inspect --json
sanho pull --json
sanho doctor --json
```

`init`, `clean`, `project`, `workspace`, `migrate`, and direct `hook`
entrypoints do not have a JSON success document. `diff` also has no JSON mode;
its patch, diffstat, or path output is intended for direct inspection. Use
`log --json` for machine-readable history and publication provenance, and
`show --json` for what one canonical commit publishes.

## Common rules

- A successful command writes one two-space-indented JSON object followed by a
  newline. The `version` compact-output exception is documented in its section.
- Human guidance and diagnostics stay on stderr when `--json` is active.
- A failed JSON command writes one error envelope to stdout and keeps its normal
  non-zero exit code and stderr guidance. This includes a failure the command
  never got to report: a rejected flag, an unparseable flag value, or an
  unexpected positional argument is answered with `invalid_arguments` on stdout,
  not with an empty one.
- The commands above are the ones that owe an envelope. A command with no JSON
  document writes its refusal to stderr only, even when `--json` was typed.
- `--help` is not a failure. It prints usage on stdout and exits 0 whether or not
  `--json` is present.
- Empty arrays are `[]`, never `null`.
- Optional objects such as `base` are `null` when unknown.
- Machine OIDs are full length; 12-character OIDs are human-output only.
- Consumers must branch on fields, not whitespace or human prose.

## Error envelope

```json
{
  "error": {
    "code": "sync_required",
    "message": "docs synchronization is required before pushing"
  }
}
```

Stable codes are:

| Code | Meaning |
|---|---|
| `not_in_workspace` | The command requires a managed workspace |
| `v1_workspace` | A legacy workspace must be converted before this command |
| `sync_in_progress` | A conflicted sync exists, or a sync-only action has no valid window |
| `sync_required` | Local and canonical docs must be reconciled |
| `docs_dirty` | The requested operation needs clean or committed docs |
| `history_rewritten` | The recorded base disappeared from canonical history |
| `unknown_target` | A requested recovery or inspection target is not usable |
| `canonical_unreachable` | Canonical fetch, merge, or publication could not run |
| `registry_lock_timeout` | Registry locking exceeded the bounded wait |
| `clone_missing` | The private canonical clone must be recreated with `sanho sync` |
| `markers_present` | Relevant docs contain complete conflict markers |
| `too_large` | A text file exceeded the marker-scan safety limit |
| `config_corrupt` | `.sanho.json` exists but is not a valid supported config |
| `base_corrupt` | The recorded base exists but is invalid |
| `base_not_corroborated` | Sanho cannot prove a proposed base matches the docs |
| `invalid_arguments` | The invocation is malformed: an incomplete or invalid flag or policy combination, an unknown flag, an unparseable flag value, or an unexpected positional argument |
| `inspection_unavailable` | Inspection cannot safely obtain a required fact under its execution policy |
| `internal` | Sanho encountered an internal defect |

The compatibility code `v1_workspace` is part of the current v0.2 error
vocabulary. It does not make the legacy layout an active operating mode.

### Additive error details

Existing `error.code`, `error.message`, exit codes, stdout/stderr separation,
and successful command documents remain compatible. Errors in the following
sync families gain three fields together: `reason`, `paths`, and `recovery_id`.
Existing errors outside the table keep their current envelope; consumers must
accept both forms and ignore unknown additive fields. The new inspection-only
availability error is specified separately below and changes no existing code.
Do not retrofit these fields onto `version` or unrelated success documents.

```json
{
  "error": {
    "code": "sync_in_progress",
    "message": "the resolution changed paths that did not conflict: docs/architecture.md",
    "reason": "non_conflict_paths_changed",
    "paths": ["docs/architecture.md"],
    "recovery_id": "sync_review_unverified_resolution"
  }
}
```

`reason` is a stable diagnosis, not wording parsed from `message`.
`recovery_id` identifies the shared CLI guidance catalog entry; it is not a
command, execution plan, permission, or promise that every prerequisite is
already satisfied. Its value is null when no catalog recovery is selected;
null does not mean the operation succeeded or recovery is unnecessary. The
same guidance definition must serve the human message and the machine
identifier. Several reasons may share a recovery sequence.

| Reason | Existing error code | Recovery ID | Meaning |
|---|---|---|---|
| `active_sync` | `sync_in_progress` | `sync_inspect_active` | A new sync or pull is blocked by an existing sync |
| `no_sync` | `sync_in_progress` | null | Continue was requested without a sync note |
| `sync_note_corrupt` | `sync_in_progress` | `sync_review_corrupt_note` | A present note cannot be decoded as a usable record |
| `invalid_sync_target` | `sync_in_progress` | `sync_review_corrupt_note` | A decoded note has no valid completion target |
| `markers_remaining` | `markers_present` | `sync_finish_resolution` | The whole-docs worktree scan found unresolved markers, including outside recorded conflicts |
| `resolution_uncommitted` | `docs_dirty` | `sync_finish_resolution` | Docs have staged, unstaged, or untracked work that prevents completion |
| `foreign_entry_history` | `sync_in_progress` | `sync_return_to_entry_history` | HEAD does not descend from the recorded entry HEAD |
| `non_conflict_paths_changed` | `sync_in_progress` | `sync_review_unverified_resolution` | Resolution differs from the merge result outside the recorded conflict set |
| `missing_merge_tree` | `sync_in_progress` | `sync_review_unverified_resolution` | A legacy note has no recorded merge result to verify |

The required coverage is `sync` and `pull` refusals caused by an active/corrupt
sync note, and all listed `sync --continue` refusals. Other JSON commands may
reuse these typed diagnoses when the same cause reaches their error boundary.
Do not infer a new reason from a generic error code alone. In particular,
`docs_dirty` during an ordinary sync is not automatically an uncommitted
conflict resolution. Hook stdout gains no JSON protocol.

All new `paths` arrays use repository-relative, forward-slash paths including
the configured docs directory, matching the existing sync conflict contract.
They are sorted, unique, and lossless for supported filenames, including
spaces, commas, newlines, and Unicode. Use `[]` when the diagnosis has no
path evidence; do not derive filenames by splitting error prose. New fields
never expose absolute state-file paths, credentials, or document contents.
Existing human error detail is not redefined by this restriction.

## Exit codes

| Exit | Meaning |
|---:|---|
| 0 | Success. A sync that writes conflicts also exits 0. |
| 1 | Actionable state described by stderr and, with `--json`, the envelope |
| 2 | Internal defect |

Always read `sync.status`; never infer the sync result from exit 0 alone.

## Stable vocabulary

`status` and related objects use these stable values:

- `relation.known`: whether behind/ahead could be calculated.
- `publication.known`: whether pending publication could be calculated.
- `sync_preview.known`: whether a preview was available.
- sibling relations: `same`, `behind N`, `ahead N`, `diverged A/B`, `unknown`.
- sync statuses: `up_to_date`, `synced`, `conflicts`, `completed`, `aborted`.
- doctor severities: `ok`, `info`, `warning`.
- log entry kinds: `publication`, `external`.
- publication cases: `up_to_date`, `fast_forward`, `auto_merge`, `unknown_base`.
  They label a `preview` verdict and appear in parentheses on the publication
  line a push prints.
- blocked preview verdicts: `sync_in_progress`, `markers_present`,
  `sync_required`, `history_rewritten`, `empty_publication`.

Unknown is not zero. When Sanho cannot establish a relationship it sets the
corresponding `known` field to false instead of inventing a count.

## `version`

Current source emits the complete document as standard compact JSON on one line:

```json
{"name":"sanho","version":"v0.2.8"}
```

The command writes one trailing newline. `name` and `version` are the complete
schema; the whitespace change does not alter either field. The human-readable
form is `sanho <version>`. Other successful JSON commands retain the common
indented form, and the compact exception covers only this success document: a
failed `version --json` writes the common indented error envelope like every
other command.

`sanho version --verbose` adds the exact Git identity when the executable was
built with one. Its JSON document is still compact and has exactly these
fields:

```json
{"name":"sanho","version":"v0.2.8-dev.0123456789ab","git_sha":"0123456789abcdef0123456789abcdef01234567"}
```

Source and ordinary release builds have no admitted commit, so `git_sha` is
`null`; the human form says `git_sha unknown`. The non-verbose forms keep the
two-field document and `sanho <version>` output unchanged.

The root `--verbose` flag and its `-v` shorthand select the same version
diagnostic before or after the command name. These forms are equivalent:

```bash
sanho --verbose version
sanho -v version
sanho version --verbose
sanho version -v
```

## `status`

```json
{
  "project": "product",
  "workspace_id": "product:/Users/name/work/app",
  "base": {
    "commit": "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
    "tree": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6"
  },
  "canonical": {
    "head": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "tree": "7b51f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "empty": false,
    "fetched_ever": true,
    "data_age_seconds": 12,
    "publication_url": "git@github.com:example/example-docs.git",
    "publication_branch": "main"
  },
  "relation": {"known": true, "behind": 2, "ahead": 0},
  "publication": {"known": true, "pending": false},
  "sync_preview": {"known": true, "clean": true, "conflicts": []},
  "working_copy": {"known": true, "docs_clean": true},
  "local_readiness": {
    "sync": {"ready": true, "blocked_by": []},
    "pull": {"ready": true, "blocked_by": []}
  },
  "sync_in_progress": false,
  "siblings": []
}
```

`status` uses the last fetched canonical snapshot. `--refresh` fetches first.
`data_age_seconds` reports cache age and is 0 when no successful fetch has ever
been recorded. `base` is null when no base is established. The publication axis
is local and independent from the canonical relationship.

Sibling rows have `workspace_id`, `base_commit`, `base_tree`, `vs_mine`,
`vs_head`, `actor_email`, and RFC3339 `last_updated_at`. They are observations
from the registry and may be `unknown` when the local clone lacks their objects.

`working_copy` covers staged, unstaged, and untracked paths under the configured
docs directory. `local_readiness` applies the same local guard precedence as
the command: `sync_in_progress`, `docs_dirty`, `working_copy_unknown`,
`no_base`, or `local_docs_changed`. `blocked_by` is always an array and is empty
when `ready` is true. These fields do not test network access or promise that a
future canonical fetch will preserve the cached relation.

## `state`

```json
{
  "home": "/Users/name/.sanho",
  "scope": "product",
  "projects": [
    {
      "name": "product",
      "docs_repo_url": "git@github.com:example/example-docs.git",
      "head": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f"
    }
  ],
  "workspaces": [
    {
      "workspace_id": "product:/Users/name/work/app",
      "project": "product",
      "local_path": "/Users/name/work/app",
      "base_commit": "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
      "base_tree": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
      "actor_email": "dev@example.com",
      "last_updated_at": "2026-08-07T09:14:03Z"
    }
  ]
}
```

Inside a workspace, the default scope is its project. `--all` uses `all`, and a
command outside a workspace also lists all registrations. `projects[].head` is
optional and appears only when a current workspace clone can provide it.
Projects and workspaces are sorted for stable output.

For inventory before conversion, `state` can project the supported legacy
registry into this schema in memory. The read leaves both registry files
byte-identical; any writer still refuses that schema.

## `log`

```json
{
  "branch": "main",
  "fetched_ever": true,
  "data_age_seconds": 12,
  "entries": [
    {
      "commit": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
      "tree": "7b51f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
      "committed_at": "2026-08-14T09:14:03Z",
      "subject": "[SANHO] Publish docs from app/main (2 app commits)",
      "kind": "publication",
      "source": {
        "repository": "app",
        "branch": "main",
        "workspace_id": "product:/Users/name/work/app",
        "application_commit": "3f0d1a5c7e21e2a3b4c5d6e7f8091a2b3c4d5e6f"
      },
      "application_subjects": ["docs: update the API guide", "docs: fix a typo"]
    }
  ]
}
```

Entries are newest first, bounded by `--max-count` (`-n`, default 20).
`--path` narrows to one docs-root-relative path, `--repository` and
`--workspace` narrow to the publications one application repository or one
workspace sent, and `--refresh` fetches before reading. Multiple narrowings use
AND semantics. `branch` is the publication branch the entries come from.

A narrowing flag given an empty value reports `invalid_arguments` rather than
being read as no filter at all.

Only a `publication` entry carries a source, so an `external` commit matches no
source filter. The filter runs first inside git, over the message text, which
is what keeps `--max-count` bounding the entries listed; each match is then
confirmed against its decoded provenance. A commit whose message coincidentally
contains the filter text is therefore dropped, so a filtered listing can be
shorter than `--max-count` even when further matches exist deeper in history.
Read `entries` rather than inferring exhaustion from its length.

`kind` is `publication` when the commit carries the publication provenance
Sanho writes, and `external` for a commit made directly in the canonical
repository. An `external` entry has `source: null` and an empty
`application_subjects`; both fields describe a record that does not exist
rather than an empty one. `committed_at` is RFC3339 in UTC.

Unlike `diff`, `log` does not require a recorded base: it is the listing the
rewrite-recovery guidance names, and that state is precisely where no base
resolves. A canonical repository with no commits reports `entries: []` rather
than an error.

## `show`

Listing mode names no document:

```json
{
  "commit": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
  "tree": "7b51f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
  "path": null,
  "entries": [
    {
      "path": "api.md",
      "mode": "100644",
      "oid": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
      "size": 1234
    }
  ],
  "document": null
}
```

`--path <document>` fills the other half:

```json
{
  "commit": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
  "tree": "7b51f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
  "path": "api.md",
  "entries": [],
  "document": {
    "oid": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
    "size": 1234,
    "binary": false,
    "content": "# API\n"
  }
}
```

One shape covers both modes so a consumer never has to know which one it asked
for: `path` and `document` are null in listing mode and `entries` is `[]` in
document mode, which is the usual reading — an absent record is null, an empty
collection is `[]`.

`document.content` is null exactly when `document.binary` is true. Sanho
classifies what it reads and never returns unclassified bytes, so "this is a
40 KiB image" is the complete answer rather than a truncated one. In human
output a binary document writes its note to stderr and leaves stdout empty,
which is what keeps a redirect of the text case exact.

The `<commit>` argument accepts what `sync --rebase-onto` accepts: a full or
abbreviated OID, or a ref such as `refs/remotes/origin/main`. A revision the
private clone cannot resolve, a path the commit does not publish, and a path
naming a directory all report `unknown_target`. Text past the marker scan
limit reports `too_large`. `--refresh` fetches before reading; like `log`, the
command needs no recorded base.

## `sync` and `pull`

```json
{
  "status": "synced",
  "base": {
    "commit": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "tree": "7b51f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f"
  },
  "commit": "a51f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
  "conflicts": [],
  "merge_drift": 0
}
```

| Status | Meaning |
|---|---|
| `up_to_date` | No docs or base change |
| `synced` | Canonical docs were applied, with `commit` when one was created |
| `conflicts` | Markers and a sync note were written; inspect `conflicts` |
| `completed` | `sync --continue` adopted `base` and cleared the note |
| `aborted` | `sync --abort` restored docs from current `HEAD`, restored or cleared the previous base, and cleared the sync note; existing commits remain |

`merge_drift` is non-zero only when a completed resolution differs from the
merge result on recorded conflict paths. A difference on any other path rejects
completion with the standard `sync_in_progress` failure envelope and leaves the
sync note and previous base in place. `pull --json` uses the same success schema.

## `preview`

```json
{
  "branch": "main",
  "tip": "3f0d1a5c7e21e2a3b4c5d6e7f8091a2b3c4d5e6f",
  "canonical": {
    "head": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "empty": false,
    "fetched_ever": true,
    "data_age_seconds": 12
  },
  "verdict": "fast_forward",
  "publishes": true,
  "blocked": false,
  "conflicts": []
}
```

`verdict` is what the push would be decided as. When `blocked` is false it is a
publication case — `up_to_date`, `fast_forward`, or `auto_merge` — and
`publishes` says whether a canonical commit would be created; an up-to-date
push is decided and publishes nothing. When `blocked` is true the push would be
rejected and `verdict` is `sync_in_progress`, `markers_present`,
`sync_required`, `history_rewritten`, or `empty_publication`.

`conflicts` lists the docs paths the verdict names: the conflicted paths for
`sync_required`, the marker-carrying paths for `markers_present`, and `[]`
otherwise.

**Preview exits 0 whenever it reached a verdict, including a blocked one.** A
rejection is the answer, not a failure of the command, and a diagnostic that
failed on unwelcome news could not be used to ask. Branch on `blocked` and
`verdict`, never on the exit code. Only a failure to reach a verdict — an
unreachable canonical, a missing clone — produces an error envelope and exit 1.
Use `check` when automation needs a gate.

`--branch` previews another local branch; a name this repository does not have
reports `unknown_target`, and an empty value reports `invalid_arguments`. A
push of several branches at once is not previewed. `--refresh` fetches first:
without it the verdict describes the last fetched snapshot, while the pre-push
hook always fetches, so read `canonical.data_age_seconds` before relying on a
cached verdict.

## `check`

```json
{
  "passed": false,
  "checks": [
    {"name": "clean", "passed": true, "reason": "clean"},
    {"name": "current", "passed": false, "reason": "behind"},
    {"name": "published", "passed": true, "reason": "published"}
  ]
}
```

At least one of `--require-clean`, `--require-current`, or
`--require-published` is required. Checks appear in that fixed order when
selected and use AND semantics. Policy mismatch still writes this result and
exits 1; it is not an error envelope. Failures that prevent evaluation, such
as an unreachable canonical required by `--require-current`, use the standard
error envelope instead.

Stable reasons are `clean`, `current`, `published`, `canonical_empty`,
`docs_dirty`, `no_base`, `relation_unknown`, `behind`, `ahead`, `diverged`,
`publication_pending`, and `sync_in_progress`.

## `doctor`

```json
{
  "workspace": "/Users/name/work/app",
  "checks": [
    {"name": "hooks", "severity": "ok", "detail": "all 6 hooks installed exactly once"}
  ],
  "warnings": 0
}
```

`warnings` counts only `warning` rows. `info` describes a healthy but noteworthy
state. Doctor exits 0 when it finds warnings so automation can consume the full
report; it fails only when diagnosis itself cannot run.

## Sync inspection

`sanho sync --inspect` exposes the shared local completion assessment. The
[EPIC-003 roadmap](roadmap/README.md#epic-003-structured-diagnostics-and-sync-inspection)
records the feature's delivery and acceptance status.

### Inspection command and result

Run the read-only mode with:

```bash
sanho sync --inspect
sanho sync --inspect --json
```

`--inspect` is mutually exclusive with `--continue`, `--abort`, and
`--rebase-onto`. Reject a supplied `--rebase-onto` even when its value is empty
in an inspection invocation. Invalid combinations return `invalid_arguments`
before any workspace mutation, clone creation, or network access. Inspection
has no `--refresh`, repair, apply, or automatic completion option.

Inspection diagnoses the active sync from the application repository and its
worktree-local note. It needs neither a canonical clone nor network access.
It reports whether the *local completion checks* pass, not whether a later
base write, filesystem operation, or push will succeed. A missing clone or an
unavailable canonical remote does not prevent inspection when the local
objects needed for the assessment are present and its execution policy can
safely obtain the required facts. Inspection does not run external clean or
process filters. The initial policy conservatively refuses worktree evaluation
when any such command is configured, even if it might not apply to docs; see
[the execution boundary](architecture.md#read-only-boundary). Built-in Git
attribute conversions remain supported. This inspection restriction does not
remove existing filter support from Continue.

A blocked example follows. OIDs here are illustrative; actual output uses full
OIDs accepted by the repository.

```json
{
  "state": "active",
  "head": "3333333333333333333333333333333333333333",
  "note": {
    "previous_base": {
      "commit": "1111111111111111111111111111111111111111",
      "tree": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    },
    "target": {
      "commit": "2222222222222222222222222222222222222222",
      "tree": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    },
    "entry_head": "4444444444444444444444444444444444444444",
    "entry_docs_tree": "cccccccccccccccccccccccccccccccccccccccc",
    "merged_tree": "dddddddddddddddddddddddddddddddddddddddd",
    "conflicts": ["docs/api.md"]
  },
  "checks": [
    {"name": "sync_note", "state": "passed", "reason": null, "paths": []},
    {"name": "markers", "state": "passed", "reason": null, "paths": []},
    {"name": "docs_clean", "state": "passed", "reason": null, "paths": []},
    {"name": "entry_history", "state": "passed", "reason": null, "paths": []},
    {"name": "merge_tree", "state": "passed", "reason": null, "paths": []},
    {
      "name": "non_conflict_preservation",
      "state": "blocked",
      "reason": "non_conflict_paths_changed",
      "paths": ["docs/architecture.md"]
    }
  ],
  "comparison": {
    "expected_tree": "dddddddddddddddddddddddddddddddddddddddd",
    "actual_tree": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
    "allowed_changes": ["docs/api.md"],
    "unexpected_changes": ["docs/architecture.md"]
  },
  "continuation": {
    "ready": false,
    "reason": "non_conflict_paths_changed",
    "paths": ["docs/architecture.md"],
    "recovery_id": "sync_review_unverified_resolution"
  }
}
```

The result obeys these rules:

- `state` is `none`, `active`, `corrupt`, or `changed`. `none` means no note;
  `corrupt` means a present but undecodable or invalid-target note; `changed`
  means an observed HEAD or note change invalidated the assessment during the
  read. A supported legacy note missing only its merge-tree/entry fields is
  `active`, not automatically corrupt.
- `head` is the assessed application HEAD, or null for a positively verified
  unborn HEAD. A valid detached HEAD retains its OID. A missing referenced
  object, broken HEAD/ref, or failed Git command is an error, not unborn.
  `note` is null for `none`, `corrupt`, or `changed`. Unknown/unrecorded OIDs
  and `previous_base` without a recorded previous base are null, not invented
  empty identifiers. A note's target is not the currently adopted base.
- `checks` always contains the six names above in that order. Each state is
  `passed`, `blocked`, `not_evaluated`, or `not_applicable`. Stop at the first
  completion blocker; later checks are `not_evaluated`, not passed. Reuse the
  actual Continue guard order. Its marker check scans regular files throughout
  the configured docs worktree, including untracked/ignored files and paths
  outside `note.conflicts`. That recorded set bounds only the later allowed
  changes. Missing entry provenance that current Continue accepts is
  `not_applicable` with the check-only reason
  `entry_history_unrecorded`; it is neither proven ancestry nor a new blocker.
- `comparison` is null unless the merge comparison ran. Otherwise it reports
  the recorded merge tree and the comparable current docs tree after the
  clean-docs guard, together with changed paths partitioned by the *recorded*
  conflict set. Obtain the actual tree through the admitted, semantics-preserving
  worktree comparison; do not substitute HEAD content or filtered-off raw
  bytes. An unchanged local-side resolution can be valid. A positive
  `allowed_changes` count is not a requirement for readiness. Do not widen the
  conflict set to make a changed path appear acceptable.
- `continuation.reason` is the first blocking reason from the table, with
  matching `paths` and `recovery_id`. For `none`, it is `no_sync` with empty
  paths and null recovery. When ready, it is null, paths are empty, and recovery
  is null. Readiness never grants permission to run Continue.
- For `changed`, `head`, `note`, and `comparison` are null, all checks are
  `not_evaluated`, and continuation is not ready with the inspection-only
  reason `observation_changed`, empty paths, and null recovery. Discard the
  invalidated snapshot; a fresh read is needed. Do not retry indefinitely.
- Check-only and inspection-only reasons are not new global error codes.
  Arrays are always arrays. Human output safely quotes unusual paths and
  shows the same decision and evidence without claiming unseen checks passed.
  The separately specified `inspection_unavailable` is an error envelope,
  not a fifth result state or a seventh completion check.

The assessment is a bounded observation, not a locked snapshot or execution
receipt. Compare HEAD and the note's identity/content before and after the
assessment and report detected changes. This does not claim to detect every
possible concurrent edit. Continue always performs a fresh assessment; it
never consumes or trusts an earlier inspection result.

Inspection exits 0 when it produces a diagnosis, including `none`, `corrupt`,
`changed`, or a blocked active sync. Failures that prevent diagnosis, such as
Git execution failure, missing required local objects, unreadable repository
state, or a text scan limit, retain the normal error envelope and exit-code
classification. Do not report those failures as clean or ready. In particular,
a malformed sync note is a diagnosable corrupt state, while an unrelated I/O
failure must not be silently reclassified as note corruption. HEAD/object reads
must distinguish a verified unborn branch or genuinely absent docs subtree
from existing refs/trees with missing objects. Failed `read-tree` must not fall
back to an empty index unless unborn HEAD was positively established. Apply
this rule to the shared reader implementations, not just the JSON renderer.

### Inspection availability errors

When a required fact cannot be obtained under the read-only execution policy,
inspection exits 1 and emits exactly one error envelope instead of a partial
success report:

```json
{
  "error": {
    "code": "inspection_unavailable",
    "message": "Read-only inspection cannot assess this Git filter configuration.",
    "reason": "external_filter_configured",
    "paths": [],
    "recovery_id": null
  }
}
```

This new code is limited to `sync --inspect` and has two stable reasons:

| Reason | Meaning |
|---|---|
| `external_filter_configured` | Effective configuration contains a non-empty clean/process command; the initial conservative policy stops before a filter-capable read |
| `execution_policy_unavailable` | Required local-only, non-executing reader controls cannot be established, or a detected configuration change invalidates their admission |

For both reasons, `paths` is `[]` and `recovery_id` is null. Configuration-level
refusal must not invent affected document paths or expose configured commands,
credentials, or absolute paths. Human output states that assessment was not
performed, not that docs are dirty, corrupt, or ready. Operational Git/I/O
failures use their normal classifications; they are not converted to this code
merely to hide an implementation error.

Apply admission only when the ordered assessment reaches a read that needs it.
A valid no-note/corrupt-note diagnosis or an earlier marker blocker is returned
without attempting a later status/normalization operation. Markers outside the
recorded conflict set still take precedence. Do not probe a prohibited filter
to find out whether the later clean-docs check would pass.

On unchanged inputs successfully evaluated by both surfaces, inspection and
Continue must agree. `inspection_unavailable` means no equivalent verdict was
obtained, not that Continue would reject. Do not alter Git filter configuration,
retry with filters disabled, fall back to ordinary `status`/`sync` as an escape,
or automatically invoke Continue. Any further mutation remains subject to the
existing authorization and guard contracts. The fixed availability message
need not name a recovery command; keep it at the CLI message boundary rather
than inventing an automatic catalog recovery.

A failed real Continue still returns its normal error envelope and nonzero
exit code. Its existing success document, including `merge_drift`, is unchanged.
Inspection does not introduce another value into the existing sync `status`
vocabulary. File-content/patch viewing is not required by this Epic: tree OIDs
and exact allowed/unexpected paths are the initial comparison evidence.

## Automation rules

1. Parse stdout as one JSON document and read the process exit separately.
2. On failure, branch on `error.code`, then show `error.message` and stderr.
3. On sync success, branch on `status`; `conflicts` requires resolution and
   `sanho sync --continue`.
4. Treat every `known:false` as unavailable data, not a zero relationship.
5. Treat `sync_preview` as a committed-tree prediction and `local_readiness` as
   current local preconditions; neither guarantees a later network operation.
   Read a `preview` verdict the same way: it describes the snapshot it was
   decided against, and the pre-push hook fetches again.
6. On `preview`, branch on `blocked` and `verdict`, never on the exit code: a
   push that would be rejected is reported at exit 0.
7. Never parse human tables or short OIDs.
8. Never bypass a named recovery action with force, manual state edits, or
   `--no-verify`.

Related documents: [Architecture](architecture.md),
[Operations](operations.md), and [Recovery](recovery.md).
