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
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.3
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.3
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
reconcile `.sanho_docs_hash` and daemon workspace state after merge, rewrite,
or branch checkout when the resulting docs tree matches a reachable
`docs-version` commit. Large rebase mapping sets use one batched object check
and one batched reachability check under a dedicated reconciliation timeout.
The resulting private validation permit is bound to the worktree, rewritten
HEAD, rewrite command, and exact ordered mapping set, so reconciliation does
not repeat a Git process for each rewritten commit. Optional fields after each
old/new object ID pair are accepted as opaque Git metadata without weakening
source provenance or commit validation. If a commit or rewrite hook is interrupted,
`sanho pull-commit --recover` classifies the transaction and creates recovery
refs before clearing only state that can be proven complete. `sanho status`
shows the active phase and exact safe next command, and pre-push continues to
block ambiguous or incomplete transactions. A generated docs sync commit stays
pending for application-repository publication. On `git push origin main`, the
original push publishes the complete local main history. On another origin
branch push, pre-push first fast-forwards the complete local main branch to
`origin/main`, then allows the requested branch push. Main rejection or
divergence blocks the target push without force-pushing; retry the same
`git push` after resolving the cause. On the first pending publication from an
older workspace hook, Sanho upgrades the hook in place and asks for the same
push once more. While main publication is pending, branch pushes through an
alias remote or direct URL are blocked until `git push origin main` succeeds;
tag-only pushes and deletions are unaffected.
Run `sanho <command> --help` for the complete interface.
Machine-readable output for query commands is documented in
[CLI JSON output](docs/cli-json.md).
Release checks that require real remotes, branch rules, or service managers are
documented in the [hands-on test checklist](docs/hands-on-testing.md).

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
- [한국어 안내](docs/readme/kor.md)

Historical Web, PTY, session, and agent roadmaps were removed because they no
longer describe this product.
