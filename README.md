# Sanho

![Sanho synchronizing application documentation with a canonical Git repository](.github/assets/sanho-hero.png)

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
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.6
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

### Optional: configure an AI coding agent

Installing Sanho does not modify a project's `AGENTS.md` and does not install
an agent skill. Sanho works normally without either integration. You may use
the template below, the source-distributed skill, both together, or neither.

Copy only these essential project-wide rules into the applicable `AGENTS.md`:

```markdown
## Sanho

This repository uses Sanho to synchronize `docs/` with a canonical Git repository.

- Do not invoke Sanho for routine editing, review, build, or test work.
- Before an authorized commit, run `sanho status --json`. Before an authorized push, run `sanho status --refresh --json`; derive any next step from that current state.
- Let Git run the installed Sanho hooks. Never bypass them with `--no-verify`, a safety-evading force operation, or manual edits to Sanho-managed state.
- Sanho status never grants permission to commit or push. Re-read status after a Sanho, commit, or push mutation and report only what the resulting evidence proves.
- Require explicit user intent before `sanho init`, `sanho clean`, `sanho migrate`, `sanho sync --abort`, `sanho sync --rebase-onto`, `sanho doctor --fix`, or `sanho project` changes.
```

For fuller conditional guidance, use the complete
[`use-sanho` skill directory](skills/use-sanho/). For Codex, install user-scoped
skills under `$HOME/.agents/skills`. Other agents may use different discovery
paths, so consult their documentation before choosing a destination. Download
only the skill directory into that agent-specific location:

```bash
(
  set -eu
  sanho_skill_parent="${HOME}/.agents/skills"
  sanho_skill_ref=v0.2.6
  mkdir -p "$sanho_skill_parent"
  sanho_skill_target="$sanho_skill_parent/use-sanho"
  test ! -e "$sanho_skill_target" && test ! -L "$sanho_skill_target"
  sanho_skill_tmp="$(mktemp -d "$sanho_skill_parent/.use-sanho.XXXXXX")"
  trap 'rm -rf "$sanho_skill_tmp"' EXIT
  mkdir -p "$sanho_skill_tmp/use-sanho/references"
  sanho_skill_url="https://raw.githubusercontent.com/irootkernel/sanho/$sanho_skill_ref/skills/use-sanho"
  curl -fsSLo "$sanho_skill_tmp/use-sanho/SKILL.md" "$sanho_skill_url/SKILL.md"
  for reference in lifecycle authoring recovery; do
    curl -fsSLo "$sanho_skill_tmp/use-sanho/references/$reference.md" \
      "$sanho_skill_url/references/$reference.md"
  done
  mv "$sanho_skill_tmp/use-sanho" "$sanho_skill_target"
)
```

The skill is source-distributed: a source archive made from a revision that
contains it includes the directory. `sanho_skill_ref` selects a release that
distributes the skill; earlier tags, including `v0.2.4`, do not contain it.
`go install` installs only the `sanho` binary and does not copy or register the
skill.

### Initialize a workspace

Run this at the **root** of an application Git repository:

```bash
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

If `example` was already registered with `sanho project add` or by another
workspace in the same `SANHO_HOME`, the URL may be reused:

```bash
sanho init --project example
```

An explicit URL must match the registered value.

`init` registers the project, writes `.sanho.json`, creates a private clone of
the docs repository inside `.git/sanho/canonical`, installs six Git hooks, and
adds Sanho's state files to `.gitignore`. Then, depending on what it finds:

- Canonical has content and you have no local `docs/` — canonical's docs are
  checked out and staged. You make the commit.
- Canonical is empty — no base is recorded, and your first push publishes.
- You already have local `docs/` — the base is derived from the provenance
  already in your repository's history. Your files are never touched.

#### What belongs in Git

Commit the `.gitignore` update made by `sanho init`, but do not commit Sanho's
workspace state. Files such as `.sanho.json`, `.sanho_base.json`, the legacy
`.sanho_docs_hash`, their backups, pending-operation files, and atomic-write
temporary files describe one local checkout and remain ignored.

In particular, `.sanho.json` is not a shared project configuration file. It
contains checkout-specific identity, actor, and hook-placement information in
addition to the project name, docs repository URL, and docs directory. Removing
it from `.gitignore` or copying it between clones would make distinct
workspaces claim the same local identity and assumptions.

Each independent clone is therefore initialized separately with `sanho init`,
even when it belongs to the same project. Linked Git worktrees are the narrow
exception: they share the main worktree's configuration and private canonical
clone through the common Git directory while retaining worktree-local sync and
base state.

To give every contributor the same onboarding inputs, record the standard
`sanho init --project ... --docs-repo-url ...` command in the project's README
or `AGENTS.md`; do not publish Sanho's generated state as configuration. What
the repository shares is the tracked `docs/` tree and its Sanho provenance in
Git history, not a workspace's local Sanho state.

For a repository-local custom `core.hooksPath` or a recognized Husky 9 layout,
add `--manage-custom-hooks`. This explicit opt-in preserves existing scripts
and uses portable `sanho` invocations; global or repository-external hook paths
remain unsupported.

### The daily loop

```bash
sanho status     # where am I
sanho diff       # inspect incoming cached canonical changes
sanho log        # what changed in canonical, and which repository sent it
sanho check      # enforce explicit local/current/published policies
sanho sync       # reconcile with upstream
sanho pull       # just take canonical's docs (no local edits)
git push         # publish — there is no `sanho push`
```

Status separates the committed-tree merge preview from local command
readiness. A clean preview says how `HEAD` would merge; `sync now` and `pull
now` say whether the docs working copy passes those commands' current local
guards. They do not predict network availability or a later fetch.

`sanho log` answers the other question — where a document came from. Every
publication records its source repository, branch, workspace and application
commit, and `log` reads them back:

```text
9a41f2cbbbbb  2026-08-14  [SANHO] Publish docs from app/main (2 app commits)
              from product:/Users/me/work/app @ 3f0d1a5c7e21
                - docs: update the API guide
                - docs: fix a typo
5a2b0c1d4e6f  2026-08-12  docs: reworded upstream
```

The second line is absent for a commit made directly in the canonical
repository; `--json` reports those as `"kind": "external"` with a null `source`.
`--path <document>` narrows the listing to one file. Like `status` and `diff`,
`log` reads the last fetched snapshot unless you pass `--refresh`, and unlike
`diff` it needs no recorded base — it is what the rewrite-recovery message
tells you to run when the base can no longer be resolved.

A commit on a stale base prints one line and succeeds:

```text
sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly
```

Silence means you are up to date. When you see the warning, run `sanho sync`. On
a clean merge it writes one `[SANHO] Sync docs to <oid12>` commit with your Git
identity:

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
sanho diff      # inspect incoming or local docs changes       (--refresh, --local, --stat, --name-only)
sanho log       # canonical history and its publication source (--refresh, -n, --path, --json)
sanho check     # explicit CI policy checks                    (--require-clean, --require-current, --require-published, --json)
sanho state     # registered projects and workspaces          (--all, --json)
sanho sync      # reconcile   (--continue, --abort, --rebase-onto <oid>, --json)
sanho pull      # fast-forward consume                        (--commit, --json)
sanho clean     # remove Sanho from this workspace  (--dry-run, --remove-docs, -y)
sanho doctor    # check this workspace                        (--fix, --json)
sanho project   # add | delete a project registration
sanho workspace # forget a stale workspace registration
sanho migrate   # legacy workspace conversion (compatibility only)
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

Release checks that need real remotes, branch protection rules, or multiple
machines are documented in the
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

All repository documentation is written in English. `docs/architecture.md` is
the current implementation authority; release history lives in the changelog
and Git history.
