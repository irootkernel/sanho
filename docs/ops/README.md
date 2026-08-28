# Operations

This index owns operational guidance for the Sanho CLI and its managed
workspaces. Repository maintainers own routine diagnosis and recovery; the
release owner owns release-candidate verification and accepts any documented
scope-based skip.

- [`../operations.md`](../operations.md) covers builds, daily workflows,
  diagnostics, policy checks, and failure response.
- [`../deployment.md`](../deployment.md) covers installation, onboarding,
  upgrades, state layout, and removal.
- [`../recovery.md`](../recovery.md) covers bounded state-changing recovery and
  post-recovery verification.
- [`../hands-on-testing.md`](../hands-on-testing.md) covers release checks that
  require real credentials, remotes, networks, or installed-binary context.

Follow the linked document that owns the target operation. Diagnose read-only
state before mutation, preserve user and Git state, verify explicit
postconditions, and use the documented rollback or recovery path. Escalate an
uncovered or ambiguous state to the repository maintainers; release verdict
exceptions require the release owner.

Never place credentials, tokens, private keys, or live secret values in this
documentation.
