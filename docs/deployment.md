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
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.7
sanho version
command -v sanho
```

For checkout development:

```bash
make cli-build       # bin/sanho
make cli-install
```

For an Aquarium development candidate, run the producer from a clean local
`main` checkout. The target archives committed bytes into a separate build
directory and keeps its output and Go caches below the required absolute empty
directory:

```bash
make aquarium-dev-describe
make aquarium-dev-build AQUARIUM_DEV_OUTPUT=/absolute/empty/directory
```

The build prints one `aquarium-dev-artifact-manifest/v1` JSON object on stdout.
Its executable is `bin/sanho`, built for Darwin arm64, with a version of the
form `<CurrentVersion>-dev.<sha12>`. Use `sanho version --verbose --json` to
compare the embedded version and full commit SHA with that manifest. Ordinary
`sanho version` output remains unchanged.

The source tree also distributes an optional `use-sanho` agent skill under
`skills/use-sanho/` for AI coding agents. It is documentation, not a deployed
artifact: installing the binary never installs or registers it.

Distribute the complete directory from one reviewed revision: `SKILL.md` and
the `authoring.md`, `inspection.md`, `lifecycle.md`, and `recovery.md` references.
The [agent setup instructions](../README.md#optional-configure-an-ai-coding-agent)
cover checkout copies and downloads into an agent-specific skill directory.
Use a checkout until a revision containing the new layout is published;
`v0.2.7` does not include the inspection reference. Copying source, releasing
Sanho, installing the binary, and activating an agent skill are separate steps.

## Use the Aquarium development channel

Aquarium can select a committed Sanho development build without replacing the
production installation. Start with a clean local `main` checkout and use its
absolute path for every manager command:

```bash
aquarium-dev diagnose --repository <absolute-sanho-checkout>
aquarium-dev enroll \
  --repository <absolute-sanho-checkout> \
  --approve-enrollment \
  --approve-hook
aquarium-dev rebuild \
  --repository <absolute-sanho-checkout> \
  --approve-build
aquarium-dev diagnose --repository <absolute-sanho-checkout>
```

Enrollment records the canonical checkout below `~/.aquarium-dev` and adds an
Aquarium-owned marker block to its native `post-commit` hook. The manager
preserves unrelated hook content. On local `main`, the hook admits the new
commit and starts an asynchronous build request. The commit remains created if
request admission or worker startup fails, and the hook reports the failure on
stderr. Commits on other branches and detached HEAD do not request a build.

The approved initial `rebuild` publishes an immutable generation at
`~/.aquarium-dev/artifacts/sanho/<full-sha>/`. The `current/sanho` selector and
stable `bin/sanho` entry point advance only after the manager validates the
manifest, embedded identity, artifact boundary, and checksum. Compare the
selected command with the launcher before using the candidate:

```bash
~/.aquarium-dev/bin/sanho version --verbose --json
aquarium-dev sanho version --verbose --json
```

The launcher executes the selected generation, preserves an existing
`CODEX_HOME` value, and prepends `~/.aquarium-dev/bin` to the child `PATH`. It
does not read or change Codex configuration or credentials. The development
selection does not replace the production Sanho binary or its state.

Run `diagnose` after enrollment, rebuilds, and any interrupted manager command.
For a foreground executable, a healthy result has matching checkout and current
Git SHAs, an owned hook, no pending generation, and a validated current
artifact. If the hook reports a worker failure or a queued request is known to
remain, fix the reported producer or host condition and run an explicitly
approved `rebuild`; a failed publication preserves the previously selected
generation.
Use the manager's reported recovery action for broken enrollment, hook, or
selector state instead of editing files below `~/.aquarium-dev` by hand.

## Verify with the Aquarium consumer

The portable `make test-int` check does not opt into the native consumer. Run
the explicit consumer check only with a disposable manager root and a clean,
reviewed Aquarium checkout:

```bash
export SANHO_AQUARIUM_ROOT=/absolute/path/to/aquarium/aquarium
export SANHO_AQUARIUM_REVISION=acef263e647445d1cf4107b241a20c41babe3c3e
make aquarium-dev-realconsumer
```

The Make target checks the physical host, the effective Go target, the
absolute-root shape, and the full lowercase revision shape before dispatching
the Go test. The test then checks that the root exists, is a clean Git checkout
at the requested revision, and contains the Aquarium CLI. It clones the
current Sanho source into a temporary fixture, adds an ignored invalid source
file to that original fixture, and enrolls the fixture with the Aquarium CLI.
It verifies the committed SHA, executable checksum, embedded `sanho version
--verbose --json` identity, and the command and current selectors. It then asks
the real manager to reject malformed, wrong-SHA, and checksum-mismatched
manifests and checks that each failure leaves the selected generation and its
bytes intact.
Generation cleanup uses the manager's `cleanup` command and stays inside the
temporary manager root. The external Aquarium checkout is read-only; Python
is invoked with `-B` and `PYTHONDONTWRITEBYTECODE=1`.

The manager gives each description and dry-run build probe a 30-second limit
and gives a full validated build a separate 600-second limit. The Sanho
consumer test measures the producer's description and build targets with a
30-second bound, using a fresh output directory with fresh Go build and module
caches for each build. It reports the native rebuild time separately, since
that measurement also includes the manager's exact clone and immutable
publication work.

The current Darwin arm64 check used Go 1.26.6 and Git 2.50.1 with the
per-output caches empty at the start of the producer build. It measured
description in 54 ms, the cold producer build in 2.71 s, and the native
consumer rebuild in 3.81 s. These values document the observed dependency and
toolchain conditions; repeat the check after changing either checkout or
toolchain rather than treating them as a release latency guarantee.

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
| `--docs-repo-url` | registered project URL | Canonical docs repository; required when the project is not registered |
| `--docs-dir` | `docs` | Repository-relative docs directory |
| `--actor-email` | Git user email | Canonical commit identity |
| `--manage-custom-hooks` | off | Opt in to repository-local custom/Husky hooks |
| `--force` | off | Replace existing docs from canonical; requires `-y` |
| `-y`, `--yes` | off | Confirm destructive behavior |

Init registers the project and workspace, writes v2 config, creates the private
clone, derives a safe base, installs six hooks, and updates `.gitignore`.

After an explicit project registration, later workspaces sharing the same
`SANHO_HOME` can omit the URL:

```bash
sanho project add example --docs-repo-url git@github.com:example/example-docs.git
sanho init --project example
```

Supplying a different URL for an existing project is rejected before workspace
state is written.

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

If a checkout directory was deleted without cleaning it first, remove its
stale registry row with `sanho workspace forget <workspace-id>`. This does not
replace `sanho clean` for a live checkout.

After every workspace is removed:

```bash
rm "$(go env GOPATH)/bin/sanho"
```

Inspect `~/.sanho` before deleting it. It contains registry state, not canonical
document data.
