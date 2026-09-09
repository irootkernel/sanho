# Integrate Sanho with the Aquarium development channel

This is the delivery dossier for Sanho
[EPIC-002](../roadmap/README.md#epic-002-integrate-sanho-with-the-aquarium-development-channel).
The roadmap alone owns Epic and Task status, ordering, and dependencies.

## Goal and ownership

Develop Sanho through `~/.aquarium-dev` using one exact committed executable.
Deliver the producer, verify its failure boundaries, integrate the approved
canonical checkout, and return the selected candidate's identity and results
to Aquarium through Sorage.

Aquarium's request is `TASK-014: Integrate Sanho with ~/.aquarium-dev`, revision
1. It maps to Aquarium EPIC-002 / TASK-014, not to Sanho's Task namespace.
Aquarium owns enrollment, hooks, immutable publication, command selection,
and consumer acceptance. Sanho owns its build implementation, runtime behavior,
and producer verification. Sanho remains an optional Aquarium producer.
SKILL-07 and Sanho EPIC-001 are separate completed work.

The canonical checkout selected for this plan is
`/Users/draccoon/Workspace/RootKernel/dolgorae/sanho`. Confirm its identity and
current state before enrollment. Local `main` may be ahead of its remote;
development acceptance does not require publication to that remote.

## Authorities

- Sanho's [architecture](../architecture.md), [CLI JSON contract](../cli-json.md),
  [deployment guidance](../deployment.md), root README, and Makefile constrain
  implementation and verification.
- `internal/buildinfo/version.go` owns the planned source version. At adoption,
  `CurrentVersion` is `v0.2.8`, matching the unreleased CHANGELOG entry.
- Aquarium's source snapshot at intake is
  `acef263e647445d1cf4107b241a20c41babe3c3e`, in
  `/Users/draccoon/Workspace/RootKernel/aquarium/aquarium`.
- In that repository, `docs/roadmap/README.md` owns external task identity;
  `docs/todo/TODO-AQUARIUM-DEV.md` owns integration acceptance;
  `plugins/aquarium/references/development-contract.md` and executable
  validators under `plugins/aquarium/tools/aquarium-dev/` own the shared
  contract and exact JSON fields.

Recheck affected external authorities when the consumer revision changes.
Report consumer incompatibilities separately instead of changing Aquarium
source or creating a parallel contract in Sanho.

## TASK-002: Producer and build identity

Implement these repository-owned interfaces:

```sh
make aquarium-dev-describe
make aquarium-dev-build AQUARIUM_DEV_OUTPUT=<absolute-empty-directory>
```

- Use the frozen v1 foreground `executable` contract with project ID `sanho`
  and artifact path `bin/sanho`. Initial platform support is Darwin arm64.
  Sanho remains one executable without a daemon or service controller.
- Description is read-only and emits one
  `aquarium-dev-producer-description/v1` JSON object. Derive `next_version`
  from Sanho's existing source version authority without a second version
  registry.
- Build admits only a clean local `main` and consumes the admitted committed
  bytes. It must work in the manager's isolated copy without untracked inputs
  or a dependency on the original checkout path.
- Require an absolute empty output directory. Contain output, temporary build
  files, and build-cache writes beneath it; reject paths that escape that
  boundary. Preserve source and unrelated host files.
- Emit one `aquarium-dev-artifact-manifest/v1` JSON object on success, with
  the full lowercase commit SHA, `v<next>-dev.<sha12>`, and the executable's
  canonical `sha256:` digest. Keep build diagnostics on stderr.
- Embed the development version and full SHA explicitly in the executable.
  Add `version --verbose` and `version --verbose --json`. Verbose JSON has
  `name`, `version`, and `git_sha`; an unknown SHA in an ordinary build is
  `null`. Human-readable verbose output identifies an unknown SHA explicitly.
  Preserve the existing plain and JSON version output when verbose is absent.
- Document the public diagnostic in the CLI JSON authority and producer
  behavior in architecture and deployment guidance. Keep future requirements
  here until implementation makes them current behavior.

Acceptance requires focused tests for description output and lack of mutation,
build admission and containment, manifest and embedded identity consistency,
and version-output compatibility. Include new packages in Makefile ownership
lists if implementation adds them. Do not defer TASK-002 correctness tests to
TASK-003.

## TASK-003: Isolated verification

Use disposable repositories and isolated Sanho state. Exercise real Git and
the existing Aquarium consumer rather than mocking below the Git boundary.
Keep all fixture development state within a disposable manager root using
the manager's supported test interface.

- Prove a successful exact-commit build, output checksum, and runtime identity
  through both producer targets and consumer validation. Show that additional
  original-checkout files cannot enter the isolated committed-source build.
- Cover dirty worktree/index, non-main or detached HEAD, invalid or nonempty
  output, containment violations, and build failure.
- Exercise consumer rejection of malformed manifests, wrong SHA, and checksum
  mismatch. Prove failed publication preserves the previously selected
  generation; do not duplicate manager implementation in Sanho.
- Measure description and build against the consumer's 30-second probe limit.
  Record cache and dependency conditions, including a cold-cache measurement.
  A measured timeout is a compatibility blocker to report to Aquarium, not
  permission to bypass its limit or weaken output containment.
- Run focused checks first, followed by configured Gaori `all` for the complete
  `make test` contract. Use fresh checkout-built binaries for CLI suites and
  isolated `SANHO_HOME` wherever registry access is possible.

Acceptance requires passing producer and consumer scenarios, recorded timing
and limitations, and the applicable repository gates. Current acceptance
failures remain in this work; they are not deferred feedback.

## TASK-004: Host integration and return handoff

Prepare each operation against current state and obtain its required approval
before execution. Use the native manager for all development-state changes.

1. Commit the verified producer candidate after explicit commit approval.
   Diagnose the canonical checkout, installed manager and launcher, enrollment,
   hook ownership, and existing production command without changing them.
2. Enroll that checkout and install the manager hook after the corresponding
   approvals. Preserve existing foreign hook bytes and permissions. Install or
   update the launcher only if needed and separately approved.
3. Run the approved initial build. Verify an immutable generation at
   `~/.aquarium-dev/artifacts/sanho/<full-sha>/`, its current selector, and the
   stable `~/.aquarium-dev/bin/sanho` command.
4. Compare `aquarium-dev sanho version --verbose --json` with direct
   `~/.aquarium-dev/bin/sanho version --verbose --json` and the manifest.
   Verify launcher environment inheritance, including `CODEX_HOME`, without
   exposing credentials or changing Codex configuration.
5. Use a later authorized local-main commit containing necessary verification
   or operational documentation to exercise native post-commit admission and
   automatic generation advancement. Do not manufacture an unrelated or empty
   trigger commit. If no substantive follow-up is ready, keep this acceptance
   item pending rather than claiming the initial build proves hook updates.
6. Verify production binary, configuration, state, existing hooks, and ordinary
   Git behavior remain intact. Use only read-only version diagnostics for host
   command-resolution checks; exercise failures in disposable fixtures.
7. Send Aquarium a bounded Sorage handoff identifying the final selected
   candidate, both target commands with exit statuses and JSON output, checksum
   proof, embedded version/SHA, focused and full verification outcomes, and the
   host checks actually performed. Include the owning Sanho task, canonical
   checkout, branch and clean-worktree evidence, and outstanding approvals or
   limitations. Separate any required consumer changes from Sanho-owned work.

Bind returned evidence to the final selected SHA after the update test. Later
commits do not inherit that evidence without checking affected facts again.
Keep native runtime evidence in its native location; do not copy raw logs,
credentials, or managed state into tracked documentation. Promote evidence
only when a downstream consumer requires a reviewed durable package.

## Execution and approval boundaries

Use the repository's Aquarium task workflow for each named task, with Podway
by default and the tracked Procedure v2 authorities. Task execution plans must
explicitly include the full Mulgae review roles: logic, security,
maintainability, product, documentation, and testing. Review execution requires
the repository's task-scoped authorization. Use Gaori for long or noisy checks.

This dossier records the approved design. Commit, enrollment, hook mutation,
development build, and launcher installation retain their distinct approval
boundaries. Carry forward approvals already granted for exact actions.
Push, release, production replacement, destructive cleanup, and managed-service
activation are outside this Epic.

All host development state belongs below `~/.aquarium-dev`, except the native
launcher's separately approved `~/.local/bin/aquarium-dev` entry. Do not write
manager state by hand, add repository-local `.aquarium` state, modify
`~/.aquarium`, or configure a separate Codex home, authentication, plugin, or
MCP installation. Development artifacts are not stable installation or release
qualification evidence. Foreground fallback is allowed only when a development
generation is absent; an invalid selected generation fails closed.

## Epic acceptance and dossier closeout

The three tasks must satisfy their acceptance criteria, followed by Epic
verification and Master's explicit acceptance. Host integration or hook-update
checks awaiting approval remain incomplete and must be reported as such.

Before Epic closeout, promote durable contracts and operating guidance to their
canonical owners, remove this dossier and its TODO index entry, replace the
roadmap's `Detailed SOT` with `Canonical Outcomes`, and then transition the Epic.
Run `make docs-check` for dossier registration and documentation changes.

Sending the handoff or accepting the incoming request does not complete
Aquarium TASK-014. Aquarium reconciles its own integration acceptance and
retains final cross-project cold validation under TASK-015.
