# Changelog

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
