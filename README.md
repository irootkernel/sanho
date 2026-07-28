# Kkachi

Kkachi keeps the `docs/` directories in application repositories synchronized
with a dedicated canonical docs repository. The product has exactly two
runtime components:

- `kkachi-server`: a small HTTP daemon that coordinates docs repository access.
- `kkachi`: a CLI used by developers and Git hooks in application workspaces.

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
make server-build
make cli-build
make server-run
```

The server listens on port `5789` by default and stores runtime state at
`data/kkachi_state.json`.

```bash
PORT=6789 STATE_FILE_PATH=/var/lib/kkachi/state.json make server-run
curl http://127.0.0.1:6789/healthz
```

For local development without building first:

```bash
make server-run-dev
```

## Initialize a workspace

Install the CLI:

```bash
make cli-install
```

Then run this in an application Git repository:

```bash
kkachi init \
  --server-url http://127.0.0.1:5789 \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

Useful daily commands:

```bash
kkachi status
kkachi pull
kkachi fix
kkachi state
```

`kkachi init` installs the Git hooks used to check, merge, and publish docs.
Run `kkachi <command> --help` for the complete interface.

## Validation

```bash
make server-test
make cli-test
# or both:
make test-all
```

The server end-to-end suite launches isolated daemons with temporary state by
default. Set `E2E_BASE_URL` only to test an explicitly selected running server.
The CLI end-to-end suite uses `http://127.0.0.1:5789` when no override is set.

## Operations and design

- [Architecture](docs/architecture.md)
- [Operations](docs/operations.md)
- [한국어 안내](docs/readme/kor.md)

Historical Web, PTY, session, and agent roadmaps were removed because they no
longer describe this product.
