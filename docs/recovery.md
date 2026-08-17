# Sanho Recovery

Recovery must preserve application history, canonical history, user changes,
and Git operation metadata. Never start with force push, `--no-verify`, manual
base edits, or deletion of `.git/sanho`.

## Start with evidence

```bash
git status --porcelain=v1
git branch --show-current
git rev-parse HEAD
git remote -v
sanho status --json
sanho doctor --json
```

Record the current base and canonical head before any state-changing command.
If a command reports an exact next command, prefer that command over a generic
recipe here.

## Complete or abort a conflicted sync

### Complete

Inspect the markers and decide the correct content from both sides:

```bash
git status
$EDITOR docs/<conflicted-file>
git add docs/
git commit -m "docs: resolve canonical sync"
sanho sync --continue
```

Continue verifies the original sync history, committed resolution, absence of
markers, clean docs state, and canonical-content absorption. It records the
target base and clears the note without creating another commit.

If continue reports merge drift, review the stated count. Drift is information
about how the final resolution differs from the original merge result, not an
automatic failure.

### Abort

Use only when the entire sync should be discarded:

```bash
sanho sync --abort
```

Abort restores the docs and base captured at sync entry and clears the note. It
does not move application refs. Preserve unrelated work before invoking it and
obtain explicit authorization.

An unreadable sync note still counts as active. Publication remains blocked,
while abort remains the supported cleanup path.

## Recover from canonical history rewrite

Start with a fresh view:

```bash
sanho status --refresh
```

If Sanho prints a candidate, execute it exactly:

```bash
sanho sync --rebase-onto <candidate>
```

An identical-tree replacement is recognized by its tree when the next docs
change is published, and that publication re-anchors the recorded base onto the
replacement history. A changed-tree rewrite needs an explicit anchor and may
produce normal conflicts. Resolve, commit, and run `sanho sync --continue`.

When no safe candidate exists, list canonical history and choose an anchor:

```bash
sanho log --refresh -n 50
sanho log --path <document>
```

Each entry names the commit, its date, and — for a Sanho publication — the
repository, branch, workspace, and application commit behind it. Narrow to a
document you recognize when the subjects alone are not enough.

Provenance settles a candidate that Sanho published. It cannot settle one
committed directly in the canonical repository: such an entry is reported as
`external` with no source, and `--path` tells you only that a commit touched a
file, not what the candidate's tree contains. Read the candidate itself:

```bash
sanho show <candidate>
sanho show <candidate> --path <document>
```

Without `--path` it lists the documents the candidate publishes; with `--path`
it prints one of them as of that commit. Both are read-only, need no recorded
base, and accept the same revisions as `--rebase-onto`, so a candidate can be
inspected in exactly the form it will be adopted in. A binary document is
reported with its size instead of being printed.

Choose an anchor only when its relationship to the workspace docs is known. Do
not use force push to recreate history solely to satisfy the old base.

## Repair a base

```bash
sanho doctor
sanho doctor --fix
```

Doctor re-derives from provenance and current docs. It writes a base only when
the guarded writer can corroborate it. If it cannot, synchronize:

```bash
sanho sync
```

Do not create or edit `.sanho_base.json` manually. A plausible OID without a
tree/content proof can make the next publication overwrite unrelated work.

## Repair hooks or the private clone

```bash
sanho doctor
sanho doctor --fix
```

Doctor can reinstall missing recognized hook lines and recreate a missing
private clone. It preserves foreign content. For custom/Husky mode it operates
only on the recorded approved directory; a changed `core.hooksPath` produces a
warning and no mutation.

If the installed executable moved, rerun doctor from the intended binary. Do
not hand-edit generated Sanho lines.

## Registry recovery

The registry is observational, but preserve it before repair:

```bash
cp ~/.sanho/state.json ~/.sanho/state.json.recovery-copy
cp ~/.sanho/state.json.bak ~/.sanho/state.json.bak.recovery-copy
```

If only the primary is corrupt and the backup is valid, restore it while
preserving mode:

```bash
cp -p ~/.sanho/state.json.bak ~/.sanho/state.json
sanho state --all --json
```

If both are corrupt, move both preserved copies aside and rebuild registration
through supported workspace commands. Do not invent workspace bases from the
registry; each workspace's `.sanho_base.json` and history are authoritative for
its derivation.

Lock timeouts are not corruption. Identify the process holding
`~/.sanho/state.lock`, let it finish or terminate it safely, then retry. Never
delete managed lock or state files merely to bypass a live owner.

For a single registry row whose checkout was already deleted, use its exact ID
instead of editing `state.json`:

```bash
sanho state --all
sanho workspace forget <workspace-id>
```

Forget changes only that observational row and refuses if its recorded path
still exists.

## Reinitialize as a last resort

Use only after preserving user changes and confirming the canonical URL and
project name:

```bash
sanho clean --dry-run
sanho clean -y
sanho init --project <name> --docs-repo-url <url>
```

`clean --dry-run` must be read-only. Reinitialization does not rewrite canonical
history, but `init --force -y` may replace local docs and therefore requires a
separate explicit decision.

## Verification after recovery

```bash
git status --porcelain=v1
sanho status --refresh --json
sanho doctor --json
```

Confirm:

- the expected application HEAD and branch did not move unexpectedly;
- `sync_in_progress` is false unless intentionally retained;
- base and canonical relationships are explained;
- publication is not pending unexpectedly;
- doctor warnings are understood;
- no user file or foreign hook content was lost.

Compatibility and release history, including retired workflows, remain in
`CHANGELOG.md` and Git history rather than this current recovery guide.
