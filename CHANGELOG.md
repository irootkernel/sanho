# Changelog

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
