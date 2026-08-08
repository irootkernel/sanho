# Sanho

Sanho keeps the `docs/` directories of multiple application repositories
synchronized with one canonical docs repository, and tells you early when they
have drifted apart.

The product is exactly one executable:

- `sanho`: a CLI used by developers and by Git hooks in application workspaces.

There is no daemon, no socket, no HTTP API, no Web UI, no browser terminal, and
no session runtime. Nothing to install, start, supervise, or lose.

## Why

Docs that live next to the code they describe get edited by whoever is holding
the code. Docs that live in one shared repository stay findable and reviewable.
Sanho lets you have both: you edit `docs/` as ordinary files in your application
repository, and Sanho publishes and reconciles that directory against a
canonical docs repository using nothing but git's own semantics.

Two sentences describe the whole model:

- **Publication happens at `git push`.** Commits are local and private, exactly
  as in plain git.
- **Detection happens at `git commit`.** The commit path reads local state only,
  prints at most one line, and never blocks.

Everything else follows. `git commit` works offline, always. Sanho never authors
a commit in your repository and never moves your refs. Conflict resolution is
the standard git idiom you already know: edit, `git add`, `git commit`.

## Requirements

- Go 1.25 or later — **to install only.** The installed binary does not need Go.
- Git. No minimum version is enforced; merge paths use `git merge-tree
  --write-tree`, which needs git 2.38 or newer in practice.
- macOS or Linux.
- Non-interactive credentials that can read and write the docs repository.
  Sanho never prompts: network operations run with `GIT_TERMINAL_PROMPT=0` and
  `ssh -o BatchMode=yes`, so a passphrase-protected key must be in ssh-agent.

Node.js and npm are not required. The only runtime dependency is cobra.

## Quickstart

### Install

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.1
sanho version
```

Go writes the binary to `GOBIN`, or to `$(go env GOPATH)/bin` when `GOBIN` is
unset. That directory must be on your interactive `PATH` for the `sanho`
command. Hooks installed by Sanho record that binary's absolute path, so GUI
Git clients do not need to reproduce the shell's `PATH`.

From a checkout:

```bash
make cli-build     # bin/sanho
make install       # go install ./cmd/sanho
```

### Initialize a workspace

Run this at the **root** of an application Git repository:

```bash
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

`init` registers the project, writes `.sanho.json`, creates a private clone of
the docs repository inside `.git/sanho/canonical`, installs six Git hooks, and
adds Sanho's state files to `.gitignore`. Then, depending on what it finds:

- Canonical has content and you have no local `docs/` — canonical's docs are
  checked out and staged. You make the commit.
- Canonical is empty — no base is recorded, and your first push publishes.
- You already have local `docs/` — the base is derived from the provenance
  already in your repository's history. Your files are never touched.

For a repository-local custom `core.hooksPath` or a recognized Husky 9 layout,
add `--manage-custom-hooks`. This explicit opt-in preserves existing scripts
and uses portable `sanho` invocations; global or repository-external hook paths
remain unsupported.

### The daily loop

```bash
sanho status     # where am I
sanho sync       # reconcile with upstream
sanho pull       # just take canonical's docs (no local edits)
git push         # publish — there is no `sanho push`
```

A commit on a stale base prints one line and succeeds:

```text
sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly
```

Silence means you are up to date. When you see the warning, run `sanho sync`. On
a clean merge it writes one ordinary commit authored by you:

```text
sanho: synced docs to 9a41f2cbbbbb (commit 3f0d1a5c7e21)
```

Then push as usual:

```text
sanho: published docs 9a41f2cbbbbb (fast_forward)
```

If a push is rejected, **no remote ref was changed.** Run the command Sanho
names, then retry the same `git push`.

## The conflict idiom

A conflicted `sanho sync` is a **success**, not a failure. It did what it was
asked to do, the markers are in your worktree, and the exit code is 0.

```text
sanho: merged docs with upstream — 2 files have conflicts:
  docs/api.md
  docs/schema.md
Resolve the markers, then:  git add docs/ && git commit
Then complete the sync:     sanho sync --continue
To undo this sync:          sanho sync --abort
```

Markers are labeled by name, not by temp path:

```text
<<<<<<< sanho-ours
my sentence
=======
the upstream sentence
>>>>>>> sanho-upstream
```

Resolve it exactly as you would any merge conflict, then say so — the same
shape as `git rebase --continue`:

```bash
$EDITOR docs/api.md docs/schema.md
git add docs/
git commit -m "docs: resolve sync conflicts"
sanho sync --continue
git push
```

The commit is ordinary git work; `sanho sync --continue` is what ends the sync.
It clears the sync note and moves the docs base to the merge target — no commit,
no network — and until you run it, `git push` is refused and says so. That is
also how you take your own side wholesale: if the docs already read the way you
want them, `sanho sync --continue` completes the sync as it stands.

Or undo it:

```bash
sanho sync --abort
```

`sanho sync --abort` **cannot fail.** It moves no ref, creates no commit, and
touches only the docs worktree and two state files. If it is interrupted, run it
again.

Every next-step command Sanho prints actually succeeds in the state where it is
printed. When no command can succeed, Sanho prints "manual intervention
required" plus diagnostics instead of a command that would fail.

## Command surface

```bash
sanho init      # register this repository as a workspace
sanho status    # base vs canonical, sync preview, siblings   (--refresh, --json)
sanho state     # registered projects and workspaces          (--all, --json)
sanho sync      # reconcile        (--abort, --rebase-onto <oid>, --json)
sanho pull      # fast-forward consume                        (--commit, --json)
sanho clean     # remove Sanho from this workspace  (--dry-run, --remove-docs, -y)
sanho doctor    # check this workspace                        (--fix, --json)
sanho project   # add | delete a project registration
sanho migrate   # convert a v0.1 workspace to the v0.2 layout
sanho hook      # hook entry points (invoked by git, not by hand)
sanho version   # (--json)
```

Exit codes: `0` success · `1` a state you can act on · `2` a bug in Sanho.

`sanho doctor` exits 0 even when it finds warnings — a diagnostic command that
fails whenever it finds a problem cannot be used to investigate one.

## Installed Git hooks

| Hook | Role | Blocks? |
|---|---|---|
| `pre-commit` | Staged marker gate, local freshness warning | Markers only |
| `commit-msg` | Stamp `docs-base` / `docs-base-tree` trailers | Never |
| `pre-push` | Publish to canonical, marker gate, sync gate | Yes — the only one |
| `post-checkout` | Re-derive the docs base after HEAD moved | Never |
| `post-merge` | Same | Never |
| `post-rewrite` | Same (amend, rebase) | Never |

Hook lines are matched by **exact line**, so foreign hook content is preserved
verbatim on install and on removal.

## Configure AI coding agents

Add the following shared instructions to the applicable `AGENTS.md` or
`CLAUDE.md` file in a Sanho-managed project:

```markdown
## Sanho workflow

This repository uses Sanho to synchronize its `docs/` directory with the canonical docs repository.

- At the start of a task and before any authorized commit or push, run `sanho status --json`. If it fails, report the error and do not bypass Sanho.
- If the repository is not initialized, stop and ask the user for the project name and docs repository URL. Do not guess these values or initialize the workspace on your own.
- Edit `docs/` as normal workspace files. Use normal Git commands and let the installed Sanho hooks run. Sanho never authors commits and never grants permission to commit or push.
- On a `sanho: docs base is N commits behind` warning, run `sanho sync`, then continue. That is the whole protocol.
- If `sanho sync` reports conflicts, it succeeded: markers are in the worktree and the exit code is 0. Resolve them, `git add`, and `git commit` as for any merge. If the correct resolution is not evident from the two sides, stop and ask the user rather than guessing.
- Never bypass Sanho with `--no-verify`, a force push used to evade a Sanho block, a `sanho push` command (it does not exist), or manual edits to `.sanho.json`, `.sanho_base.json`, `.git/sanho/`, or Sanho-owned hook lines.
- Do not run `sanho clean`, `sanho init --force`, `sanho sync --abort`, or `sanho migrate` without explicit user approval.
- When a push is rejected, read the first stderr line, run the command Sanho names, then retry the same `git push`. Sanho only ever names a command that succeeds in the state it was printed in.
- Read machine output from `--json` on `status`, `state`, `sync`, `pull`, `doctor`, and `version`. Do not parse the human-readable tables, and do not read a sync result from the exit code.
```

## Upgrading from v0.1

The canonical repository is untouched — same repository, same linear main, only
the commit-message convention changes going forward. Old `docs-version:`
trailers and `[SANHO] Update docs` commits stay as inert history, and v0.2 reads
the old trailer key, so no history rewrite is needed.

Because hook lines invoke `sanho` by name, installing the v0.2 binary routes
v0.1-era hooks to it immediately. v0.2 degrades safely rather than
half-operating: commits keep working and print one migrate hint, while `git
push` fails closed. The push boundary is the natural migration prompt.

```bash
cp ~/.sanho/state.json ~/.sanho/state.json.pre-v0.2   # back up first
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.1
cd /path/to/app && sanho migrate
```

The full procedure — including stopping the v0.1 daemon and rolling back — is in
[deployment rules](docs/deployment.md) and [recovery procedures](docs/recovery.md).

## Validation

```bash
make test
```

`make test` runs `test-prepare`, `test-unit`, `test-int`, and `test-e2e` in that
order. `test-prepare` covers generation, formatting, module verification, the
docs gate, package ownership, the architecture guardrail, vet, and lint.
Integration and e2e suites build `bin/sanho` and drive it as a black box
against throwaway git repositories; the e2e leg includes the scenario matrix
and the guidance-closure suite, which executes every next-step command Sanho
prints in the exact state that prints it.

Guidance closure is enforced, not aspirational. Every message that names a next
command lives in one catalog; a unit test parses the message file as source and
fails the build when a message is added without a catalog entry; and the suite
in `test/cli/e2e` reaches each advising state, asserts the message appears, then
runs the command it names in that state and requires success.

Release checks that need real remotes, branch protection rules, two machines, or
a real v0.1 installation are documented in the
[hands-on test checklist](docs/hands-on-testing.md).

## Documentation

- [Architecture](docs/architecture.md) — the implementation authority: runtime,
  Git, provenance, publication, sync, persistence, concurrency, and safety
  contracts.
- [Operations](docs/operations.md) — daily flows and failure response.
- [Recovery procedures](docs/recovery.md) — what to do when something is stuck.
- [Deployment rules](docs/deployment.md) — install, onboard, upgrade, remove.
- [CLI JSON output](docs/cli-json.md) — `--json` schemas and agent automation
  norms.
- [Hands-on test checklist](docs/hands-on-testing.md) — pre-release manual
  verification.
- [Changelog](CHANGELOG.md)
- [한국어 안내](docs/readme/kor.md)

Documentation under `docs/` is written in Korean; this file and `AGENTS.md` are
in English. `sanho-v0.2.md` is the v0.2 design record and is historical —
`docs/architecture.md` supersedes it as the description of what the code does.
