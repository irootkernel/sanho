# Sanho Deployment

Sanho v0.2 is a single CLI. There is no daemon, service registration, socket,
container image, or frontend asset to deploy.

## Requirements

- Go 1.25 or newer to build.
- Git at runtime. No minimum is enforced; merge paths require Git 2.38 or newer
  in practice.
- Read/write credentials for the canonical docs repository.
- Non-interactive SSH access for hook execution.

Network Git commands disable terminal prompts and use SSH BatchMode. Validate
credentials before onboarding:

```bash
GIT_TERMINAL_PROMPT=0 git ls-remote <docs-repo-url> refs/heads/main
```

## Install

Use a pinned release for reproducibility:

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.1
sanho version
command -v sanho
```

For checkout development:

```bash
make cli-build       # bin/sanho
make cli-install
```

## Onboard a workspace

Run from the application repository root:

```bash
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

| Flag | Default | Purpose |
|---|---|---|
| `--project` | required | Registry project name |
| `--docs-repo-url` | required | Canonical docs repository |
| `--docs-dir` | `docs` | Repository-relative docs directory |
| `--actor-email` | Git user email | Canonical commit identity |
| `--manage-custom-hooks` | off | Opt in to repository-local custom/Husky hooks |
| `--force` | off | Replace existing docs from canonical; requires `-y` |
| `-y`, `--yes` | off | Confirm destructive behavior |

Init registers the project and workspace, writes v2 config, creates the private
clone, derives a safe base, installs six hooks, and updates `.gitignore`.

If canonical has content and local docs do not exist, init checks out and stages
the canonical docs; the user makes the commit. If canonical is empty, the first
application push publishes. Existing local docs require provenance; Sanho never
guesses their base.

### Custom hooks and Husky

A non-default `core.hooksPath` is rejected before any workspace write unless the
user passes `--manage-custom-hooks`. Only a normalized repository-local path or
a recognized Husky 9 `.husky/_` layout is accepted. Global, external,
unrecognized, and symlinked paths remain unsupported.

Default hooks call the installed binary by canonical absolute path. Managed
custom/Husky scripts use portable `sanho` lookup because they may be tracked.
Husky generated shims are never modified.

Verify onboarding:

```bash
sanho status
sanho doctor
git status
```

## State layout

```text
~/.sanho/                         0700
  state.json                      0600
  state.json.bak                  0600
  state.lock                      0600

<application>/
  .sanho.json                     0644, ignored
  .sanho_base.json                0644, ignored
  <git-common-dir>/sanho/canonical/ 0700
  <git-dir>/sanho/sync.json       active conflicts only
```

The private clone is under the Git common directory so linked worktrees share
it. The sync note is worktree-specific.

## Upgrade within v0.2

```bash
go install github.com/irootkernel/sanho/cmd/sanho@vX.Y.Z
sanho version
sanho doctor
```

Replacing a binary at the same installed path activates default hooks without
rewriting them. Custom/Husky hooks resolve `sanho` through PATH. Run `doctor` to
confirm both modes after an upgrade. Follow release-note instructions when a
release adds an idempotent workspace repair.

`sanho migrate` remains a compatibility command for legacy workspace state; it
is not part of normal v0.2 deployment. Released compatibility history is in
`CHANGELOG.md`.

## Remove

Preview first:

```bash
sanho clean --dry-run
sanho clean -y
```

Clean removes only recognized Sanho hook lines, workspace state, the private
clone when no managed linked worktree still owns it, and the registry entry.
Foreign hook content and application docs remain. Use `--remove-docs` only with
explicit authorization to remove the working docs directory.

Project registration can be removed after its workspaces are clean:

```bash
sanho project delete <project>
```

After every workspace is removed:

```bash
rm "$(go env GOPATH)/bin/sanho"
```

Inspect `~/.sanho` before deleting it. It contains registry state, not canonical
document data.
