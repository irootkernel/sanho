# CLI JSON Output

Sanho exposes stable machine-readable documents for:

```bash
sanho version --json
sanho status --json
sanho state --json
sanho state --all --json
sanho log --json
sanho check --require-<policy> --json
sanho sync --json
sanho pull --json
sanho doctor --json
```

`init`, `clean`, `project`, `workspace`, `migrate`, and direct `hook`
entrypoints do not have a JSON success document. `diff` also has no JSON mode;
its patch, diffstat, or path output is intended for direct inspection. Use
`log --json` for machine-readable history and publication provenance.

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
| `unknown_target` | A requested recovery target is not usable |
| `canonical_unreachable` | Canonical fetch, merge, or publication could not run |
| `registry_lock_timeout` | Registry locking exceeded the bounded wait |
| `clone_missing` | The private canonical clone must be recreated with `sanho sync` |
| `markers_present` | Relevant docs contain complete conflict markers |
| `too_large` | A text file exceeded the marker-scan safety limit |
| `config_corrupt` | `.sanho.json` exists but is not a valid supported config |
| `base_corrupt` | The recorded base exists but is invalid |
| `base_not_corroborated` | Sanho cannot prove a proposed base matches the docs |
| `invalid_arguments` | The invocation is malformed: an incomplete or invalid flag or policy combination, an unknown flag, an unparseable flag value, or an unexpected positional argument |
| `internal` | Sanho encountered an internal defect |

The compatibility code `v1_workspace` is part of the current v0.2 error
vocabulary. It does not make the legacy layout an active operating mode.

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

Unknown is not zero. When Sanho cannot establish a relationship it sets the
corresponding `known` field to false instead of inventing a count.

## `version`

Current source emits the complete document as standard compact JSON on one line:

```json
{"name":"sanho","version":"v0.2.6"}
```

The command writes one trailing newline. `name` and `version` are the complete
schema; the whitespace change does not alter either field. The human-readable
form is `sanho <version>`. Other successful JSON commands retain the common
indented form, and the compact exception covers only this success document: a
failed `version --json` writes the common indented error envelope like every
other command.

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
`--path` narrows to one docs-root-relative path, and `--refresh` fetches before
reading. `branch` is the publication branch the entries come from.

`kind` is `publication` when the commit carries the publication provenance
Sanho writes, and `external` for a commit made directly in the canonical
repository. An `external` entry has `source: null` and an empty
`application_subjects`; both fields describe a record that does not exist
rather than an empty one. `committed_at` is RFC3339 in UTC.

Unlike `diff`, `log` does not require a recorded base: it is the listing the
rewrite-recovery guidance names, and that state is precisely where no base
resolves. A canonical repository with no commits reports `entries: []` rather
than an error.

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
| `aborted` | `sync --abort` restored the pre-sync state |

`merge_drift` is non-zero only when a completed resolution differs from the
merge result. `pull --json` uses the same schema.

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

## Automation rules

1. Parse stdout as one JSON document and read the process exit separately.
2. On failure, branch on `error.code`, then show `error.message` and stderr.
3. On sync success, branch on `status`; `conflicts` requires resolution and
   `sanho sync --continue`.
4. Treat every `known:false` as unavailable data, not a zero relationship.
5. Treat `sync_preview` as a committed-tree prediction and `local_readiness` as
   current local preconditions; neither guarantees a later network operation.
6. Never parse human tables or short OIDs.
7. Never bypass a named recovery action with force, manual state edits, or
   `--no-verify`.

Related documents: [Architecture](architecture.md),
[Operations](operations.md), and [Recovery](recovery.md).
