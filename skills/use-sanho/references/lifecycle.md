# Sanho Lifecycle

Load this reference only for lifecycle or installation requests. All commands
below change state unless explicitly described as inspection.

## Diagnose availability

These commands are read-only:

```bash
command -v sanho
sanho version --json
sanho status --json
sanho state --all --json
```

Do not install or upgrade Sanho automatically. Outside a configured workspace,
`status --json` returns the stable `not_in_workspace` error. Treat that as
discovery, not permission to initialize.

## Initialize or register

Require the user's explicit project name and canonical repository URL before:

```bash
sanho init --project <project> --docs-repo-url <url>
sanho project add <project> --docs-repo-url <url>
```

`init` writes workspace state, creates a private clone, installs hooks, and may
stage canonical docs. `--manage-custom-hooks` is a separate opt-in described in
[authoring.md](authoring.md). `--force -y` may replace local docs and requires a
separate explicit decision. Re-read `sanho status --json`, `sanho doctor
--json`, and Git status after initialization.

## Migrate, remove, or repair

Require explicit intent before each operation:

```bash
sanho migrate
sanho clean --dry-run
sanho clean -y
sanho project delete <project>
sanho doctor --fix
```

`migrate` converts supported v0.1 workspace state and is idempotent, but it is
still a lifecycle change. Pass `--docs-repo-url` only when migration reports
that no repository URL is recorded; the flag overrides any recorded value, so
never guess it. Always inspect `clean --dry-run` before an authorized clean.
`clean --remove-docs -y`, `project delete --force`, and `init --force -y`
need specific destructive authorization, not a general request to use Sanho.
Run `sanho doctor --json` after a repair and re-read status after every change.

## Unsupported lifecycle concepts

Sanho starts no session or task and cannot replace one. It has no daemon,
service, cancellation command, reset command, or goal state. Do not map those
requests to `clean`, `init --force`, or another destructive command.

`sanho sync --abort` only abandons an active conflicted sync; it is not general
cancellation. Require explicit intent and read [recovery.md](recovery.md)
before using it. If a Sanho process itself must be interrupted, preserve Git
and Sanho state, then reconcile current state before retrying.
