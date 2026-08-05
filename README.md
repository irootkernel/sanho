# Sanho

Sanho keeps the `docs/` directories in application repositories synchronized
with a dedicated canonical docs repository. The product has exactly two
runtime components:

- `sanhod`: a small HTTP daemon that coordinates docs repository access.
- `sanho`: a CLI used by developers and Git hooks in application workspaces.

There is no Web UI, browser terminal, PTY, or session runtime.

## How it works

Each initialized application workspace records the docs commit on which its
local `docs/` tree is based. The CLI compares that commit with the canonical
remote docs HEAD through the daemon.

When a workspace publishes docs, the daemon serializes all Git operations for
that `docs_repo_id`, refreshes its clone from origin, and accepts the snapshot
only when the submitted base still matches remote HEAD. An outdated writer is
rejected and must merge or pull before retrying. Failed pushes reset the daemon
clone to origin so an unpushed local commit cannot become a false HEAD.

## Requirements

- Go 1.25 or later
- Git
- macOS or Linux
- SSH credentials that can read and write the configured docs repositories

Node.js and npm are not required.

## Build and run

```bash
make daemon-build
make cli-build
make daemon-run
```

Install both commands directly from the module:

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.6
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.6
```

The commands are written to `GOBIN`, or to `$(go env GOPATH)/bin` when
`GOBIN` is unset. Ensure that directory is on `PATH`. `sanho version` and
`sanhod --version` report the installed module version.

The daemon runs in the foreground and listens only on the Unix socket
`~/.sanho/sanhod.sock`. It stores state in `~/.sanho/state.json` and managed
docs clones under `~/.sanho/docs_repos/`.

```bash
curl --unix-socket ~/.sanho/sanhod.sock http://sanho/healthz
SANHO_HOME=/var/lib/sanho SANHO_SOCKET=/run/user/$(id -u)/sanhod.sock make daemon-run
```

For local development without building first:

```bash
make daemon-run-dev
```

## Initialize a workspace

For a checkout-local installation, use:

```bash
make install
```

Then run this in an application Git repository:

```bash
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

Use the global `--socket /absolute/path/to/sanhod.sock` option when the daemon
does not use the default socket. `sanho init` persists the resolved absolute
path as `socket_path` in `.sanho.json`.

Useful daily commands:

```bash
sanho status
sanho status --json
sanho pull
sanho pull-commit
sanho fix
sanho state
sanho state --json
```

`sanho init` installs the Git hooks used to check, merge, and publish docs.
When a commit detects a newer central docs version, the pre-commit hook creates a
`[SANHO] Update docs` commit on the latest acceptable `main`, preserves staged
and unstaged changes, and asks you to rerun the same `git commit` command.
Unpublished linear feature branches are rebased onto that commit; published,
non-linear, or diverged branches fail without changing local refs. Workspace
docs-hash reports are retried before later commit and push operations.
`pull` keeps its no-commit behavior, records the adopted docs snapshot in
private Git metadata, and the next commit materializes it through the same
`[SANHO] Update docs` flow without turning untouched remote files into staged
deletions. `pull-commit` exposes that operation proactively. HEAD-moving hooks
never mutate Sanho state while a real Git operation is active. After operation
metadata clears, post-checkout, post-merge, completed amend post-rewrite,
pre-commit, and pre-push idempotently reconcile
`.sanho_docs_hash` and daemon workspace state from a valid reachable HEAD
`docs-version`, including fast-forward and no-op rebases that do not invoke
post-rewrite. Read-only status distinguishes this pending local reconciliation
from canonical docs drift. A standalone `REBASE_HEAD` without a rebase backend
is reported as orphaned metadata and blocks normal commits with a conditional
recovery procedure instead of unusable rebase commands. If a lifecycle hook is interrupted,
`sanho pull-commit --recover` classifies the transaction and creates recovery
refs before clearing only state that can be proven complete. `sanho status`
shows the active phase and exact safe next command, and pre-push continues to
block ambiguous or incomplete transactions. A generated docs sync commit stays
pending for application-repository publication. On `git push origin main`, the
original push publishes the complete local main history. On another origin
branch push, pre-push first fast-forwards the complete local main branch to
`origin/main`, then allows the requested branch push. Before that publication,
pre-push validates every proposed non-delete branch OID's `docs-version`,
canonical ancestry, and docs tree; one invalid ref blocks the whole push. Main
rejection or divergence blocks the target push without force-pushing; retry the same
`git push` after resolving the cause. On the first pending publication from an
older workspace hook, Sanho atomically replaces the hook while preserving
custom content and permission bits and ensuring it remains executable, then
asks for the same push once more. While main publication is pending, branch pushes through an
alias remote or direct URL are blocked until `git push origin main` succeeds;
tag-only pushes and deletions are unaffected.
Run `sanho <command> --help` for the complete interface.
Machine-readable output for query commands is documented in
[CLI JSON output](docs/cli-json.md).
Release checks that require real remotes, branch rules, or service managers are
documented in the [hands-on test checklist](docs/hands-on-testing.md).

## Configure AI coding agents

Add the following shared instructions to the applicable `AGENTS.md` or
`CLAUDE.md` file in a Sanho-managed project:

```markdown
## Sanho workflow

This repository uses Sanho to synchronize its `docs/` directory with the canonical docs repository.

- At the start of a task and before any authorized commit or push, run `sanho status --json`. If it fails, report the error and do not bypass Sanho.
- If the repository is not initialized, stop and ask the user for the project name, docs repository URL, and any non-default socket path. Do not guess these values or initialize the workspace on your own.
- Edit `docs/` as normal workspace files, but use normal Git commands and let the installed Sanho hooks run. Sanho does not grant permission to commit or push.
- Never bypass Sanho with `--no-verify`, a force push used to evade a Sanho block, a `sanho push` command (it does not exist), or manual edits or removal of `.sanho_docs_hash`, `.git/sanho`, Git operation metadata, or Sanho-owned hook entries.
- Do not run `sanho clean`, `sanho init --force`, or `sanho pull --force` without explicit user approval.
- When Sanho interrupts a commit or push, inspect both `git status` and `sanho status --json`. Rerun the same command only when Sanho explicitly instructs you to; for pending main publication, follow the reported normal Git push sequence and then retry the original push.
- For conflicts or an existing pull-commit transaction, use `pull_commit.classification`, `reason`, and `next_command`. For an active Git operation, treat `git_operation.next_commands` as choices, not commands to execute automatically. Do not choose continue, abort, or quit, delete metadata, or discard work without confirming the user's intent.
```

## Validation

```bash
make test
```

`make test` runs `test-prepare`, `test-unit`, `test-int`, and `test-e2e` in
that order. Each phase can be run independently, or narrowed to its
`-daemon`/`-client` target.

The daemon and CLI end-to-end suites launch isolated daemons with temporary
runtime homes and Unix sockets by default. Set `E2E_SOCKET` to an absolute
socket path only when testing an explicitly selected running daemon.

## Operations and design

- [Changelog](CHANGELOG.md)
- [Deployment rules](docs/deployment.md)
- [Architecture](docs/architecture.md)
- [CLI JSON output](docs/cli-json.md)
- [Operations](docs/operations.md)
- [Recovery procedures](docs/recovery.md)
- [한국어 안내](docs/readme/kor.md)

Historical Web, PTY, session, and agent roadmaps were removed because they no
longer describe this product.
