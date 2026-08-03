# Changelog

## v0.1.3 - 2026-08-03

### Added

- A hands-on release checklist now covers real remotes, branch rules, legacy
  recovery, linked worktrees, network failures, and service upgrades.
- Origin branch pushes publish pending docs system commits through the existing
  Git pre-push hook: direct main pushes keep their full history, while other
  branches publish the full local main branch first.
- `sanho status` and `sanho status --json` report pending, blocked, or corrupt
  application main publication state.
- `sanho pull-commit --recover` preserves backup refs for HEAD, the index, and
  the worktree before reconciling an interrupted transaction.
- `sanho status` and `sanho status --json` report active pull-commit phase,
  classification, and the exact safe next command.

### Fixed

- Pending main publication now blocks branch pushes through alias remotes and
  direct URLs until origin/main is published, while leaving tags and deletions
  unaffected.
- Legacy version 1 and 2 pull-commit recovery now validates the recorded merged
  docs snapshot before clearing a sibling rewrite, preserving ambiguous or
  corrupt transactions and their recovery refs.
- Main publication is fast-forward only, survives target-branch push failures,
  and retries through the same `git push` without force-pushing.
- `git commit --amend` and repeated amend operations now reconcile prepared
  pull-commit transactions through Git's `post-rewrite` mappings.
- Post-commit retry, post-rewrite, recovery, and stale-state cleanup are
  idempotent, while ambiguous transactions continue to block pushes.

### Compatibility

- Existing workspace configuration, daemon state, CLI/HTTP/JSON contracts, and
  version 3 pull-commit transactions remain compatible without reinitializing
  workspaces.
- Version 1 and 2 pull-commit transactions require their recorded merged-index
  snapshot for safe sibling-rewrite recovery; unverifiable state remains
  preserved and push-blocking.

## v0.1.2 - 2026-07-29

### Fixed

- `pull-commit` now resolves trivial three-way file outcomes before invoking the
  text merge driver, so unchanged and one-sided binary files no longer block
  documentation materialization.
- Divergent binary changes fail closed without changing HEAD, the index, the
  worktree, the local docs hash, or pull-commit transaction state.

### Compatibility

- Existing configuration, daemon state, Git hooks, and CLI/HTTP contracts remain
  compatible and do not require workspace reinitialization.

## v0.1.1 - 2026-07-29

### Fixed

- Runtime cleanup and response-writing failures are now handled explicitly.

### Changed

- Standardized Sanho-owned component terminology on `daemon` across
  documentation, CLI output, errors, internal identifiers, and tests.
- Domain and use-case boundaries are enforced by an architecture guardrail.
- `golangci-lint` v2.11.3 is pinned as a reproducible Go tool dependency.
- `make test` runs prepare, unit, integration, and end-to-end phases in order,
  with separate daemon and client targets.

### Compatibility

- Existing `.sanho.json` files, Git hooks, Unix socket paths, and daemon runtime
  state remain compatible and do not require workspace reinitialization.
- The machine-readable CLI error code `server_request_failed` is now
  `daemon_request_failed`. Automation that matches the previous code must
  update; no compatibility alias is provided.

## v0.1.0 - 2026-07-29

First public Sanho release.

### Added

- `sanho` CLI and `sanhod` daemon installable with `go install`.
- Unix-socket-only local HTTP transport.
- Private runtime state and managed docs clones under `~/.sanho/`.
- Foreground daemon lifecycle with graceful SIGINT and SIGTERM shutdown.
- User-managed launchd and systemd deployment guidance.
- Unit, integration, and end-to-end coverage for Unix socket workflows.

### Changed

- Go module path is `github.com/irootkernel/sanho`.
- Workspace configuration uses `.sanho.json` with an absolute `socket_path`.
- Workspace metadata, Git hooks, commit messages, and environment variables use
  Sanho identifiers.

### Compatibility

This is a clean-break release. It does not read or migrate Kkachi configuration,
runtime state, service registrations, or workspace metadata. Supported operating
systems are macOS and Linux.
