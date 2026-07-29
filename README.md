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
remote docs HEAD through the server.

When a workspace publishes docs, the server serializes all Git operations for
that `docs_repo_id`, refreshes its clone from origin, and accepts the snapshot
only when the submitted base still matches remote HEAD. An outdated writer is
rejected and must merge or pull before retrying. Failed pushes reset the server
clone to origin so an unpushed local commit cannot become a false HEAD.

## Requirements

- Go 1.25 or later
- Git
- SSH credentials that can read and write the configured docs repositories

Node.js and npm are not required.

## Build and run

```bash
make daemon-build
make cli-build
make daemon-run
```

The server listens on port `5789` by default and stores runtime state at
`data/kkachi_state.json`.

```bash
PORT=6789 STATE_FILE_PATH=/var/lib/kkachi/state.json make daemon-run
curl http://127.0.0.1:6789/healthz
```

For local development without building first:

```bash
make daemon-run-dev
```

## Initialize a workspace

Install the CLI:

```bash
make cli-install
```

Then run this in an application Git repository:

```bash
sanho init \
  --server-url http://127.0.0.1:5789 \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

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
`[KKACHI] Update docs` commit on the latest acceptable `main`, preserves staged
and unstaged changes, and asks you to rerun the same `git commit` command.
Unpublished linear feature branches are rebased onto that commit; published,
non-linear, or diverged branches fail without changing local refs. Workspace
docs-hash reports are retried before later commit and push operations.
`pull` keeps its no-commit behavior, records the adopted docs snapshot in
private Git metadata, and the next commit materializes it through the same
`[KKACHI] Update docs` flow without turning untouched remote files into staged
deletions. `pull-commit` exposes that operation proactively. HEAD-moving hooks
reconcile `.kkachi_docs_hash` and daemon workspace state after merge, rewrite,
or branch checkout when the resulting docs tree matches a reachable
`docs-version` commit.
Run `sanho <command> --help` for the complete interface.
Machine-readable output for query commands is documented in
[CLI JSON output](docs/cli-json.md).

## Validation

```bash
make daemon-test
make cli-test
# or both:
make test-all
```

The server and CLI end-to-end suites launch isolated daemons on ephemeral
loopback ports with temporary state by default. Set `E2E_BASE_URL` only to test
an explicitly selected running server.

## Operations and design

- [Architecture](docs/architecture.md)
- [CLI JSON output](docs/cli-json.md)
- [Operations](docs/operations.md)
- [한국어 안내](docs/readme/kor.md)

Historical Web, PTY, session, and agent roadmaps were removed because they no
longer describe this product.
