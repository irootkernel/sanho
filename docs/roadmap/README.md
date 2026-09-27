# Sanho Roadmap

This file is the sole owner of adopted Epic and Task identity, ordering,
dependencies, lifecycle vocabulary, and current status for the Sanho delivery
scope.

## Identity policy

- Epic IDs match `EPIC-[0-9]{3,}`.
- Task IDs match `TASK-[0-9]{3,}`.
- Epic and Task sequences are independent and monotonic in this roadmap.
- New IDs use the greatest number ever allocated for their kind plus one.
- IDs are never reused after completion, deletion, deferral, or migration.
- Identity does not encode execution order; roadmap order and dependencies do.

## Lifecycle

The lifecycle values are `Planned`, `In Progress`, `In Review`, `Completed`,
`Deferred`, and `Blocked`. Epic status remains independent from child Task
status and changes only after explicit Epic acceptance.

An active Epic with member Tasks links one scope-local dossier through the
`Detailed SOT` field. Closeout promotes durable outcomes, removes the dossier,
replaces that field with `Canonical Outcomes` links, and then transitions the
Epic.

## Epic summary

| Epic | Title | Status |
|---|---|---|
| [EPIC-001](#epic-001-sanho-skill-modernization-for-gpt-6-astra) | Sanho skill modernization for GPT-6 Astra | Completed |
| [EPIC-002](#epic-002-integrate-sanho-with-the-aquarium-development-channel) | Integrate Sanho with the Aquarium development channel | Completed |
| [EPIC-003](#epic-003-structured-diagnostics-and-sync-inspection) | Structured diagnostics and sync inspection | In Progress |

## EPIC-001: Sanho skill modernization for GPT-6 Astra

- Status: Completed
- Objective: Make the source-distributed Sanho skill concise and conditional
  while preserving current Git, synchronization, and authorization contracts.
- Canonical Outcomes: [Sanho skill](../../skills/use-sanho/SKILL.md),
  [distribution guidance](../deployment.md),
  [agent verification and Aquarium intake](../implementation-tips/agent-skill-verification.md)
- Dependencies: None. Aquarium integration consumes the resulting local source
  and does not block this Epic.
- External mapping: Aquarium's proposal label `SKILL-07` maps to this Epic and
  its single member Task. It is not a Sanho roadmap identifier.

| Task | Title | Status | Dependencies |
|---|---|---|---|
| TASK-001 | Restructure and verify the Sanho skill for GPT-6 Astra | Completed | None |

TASK-001 delivers the skill, affected references and distribution guidance,
directly related documentation corrections, and prepared agent verification.
Master explicitly accepted Epic closeout with the manual Astra scenarios
unperformed. Those scenarios remain available in the canonical verification
guide; completion does not claim observed agent behavior or performance gains.

## EPIC-002: Integrate Sanho with the Aquarium development channel

- Status: Completed
- Objective: Build Sanho from an exact local commit, integrate its foreground
  executable with `~/.aquarium-dev`, and return verified candidate evidence to
  Aquarium while preserving production installation and native Git behavior.
- Canonical Outcomes: [producer and verification targets](../../Makefile),
  [runtime and build contract](../architecture.md),
  [development deployment guidance](../deployment.md),
  [verbose version contract](../cli-json.md),
  [Aquarium consumer tests](../../test/aquariumdev)
- Dependencies: Aquarium's existing foreground producer contract and native
  development manager. EPIC-001 remains completed and is not reopened.
- External mapping: Aquarium EPIC-002 / TASK-014 owns consumer integration
  acceptance; Aquarium TASK-015 owns final cross-project cold validation.
  Those identifiers belong to Aquarium's separate roadmap namespace.

| Task | Title | Status | Dependencies |
|---|---|---|---|
| TASK-002 | Implement the Aquarium foreground producer and build identity | Completed | None |
| TASK-003 | Verify exact-commit builds and publication failure boundaries | Completed | TASK-002 |
| TASK-004 | Integrate the canonical checkout and hand off the verified candidate | Completed | TASK-003 |

Master explicitly accepted Epic closeout after all three Tasks completed. This
Epic delivered the producer, isolated and native-consumer verification,
approved host integration, and return handoff. Completing it does not complete
Aquarium TASK-014 or TASK-015.

## EPIC-003: Structured diagnostics and sync inspection

- Status: In Progress
- Objective: Combine structured recovery diagnoses and read-only active-sync
  inspection so operators and agents can understand the actual local Continue
  guards without parsing human error messages or retrying mutations.
- Detailed SOT: [Implementation dossier](../todo/EPIC-003-structured-diagnostics-sync-inspection.md)
- Target Contracts: [CLI JSON](../cli-json.md#planned-structured-sync-diagnostics),
  [assessment architecture](../architecture.md#planned-structured-diagnostics-and-sync-inspection)
- Dependencies: Existing sync completion guards, CLI guidance catalog, and Git
  adapters. No new external service or runtime dependency. TASK-005's existing
  recovery wording must be preserved and reconciled, but its separate review
  status is not changed or made a runtime implementation dependency.
- Scope: Additive typed error details, one shared completion assessment,
  `sanho sync --inspect [--json]`, strict inspection reader admission,
  HEAD/object-read correctness fixes, safety/compatibility regression coverage,
  and source guidance. No automatic recovery, publication preview expansion,
  workspace-wide scanner, state migration, or release-version change.
- Execution order: TASK-006, TASK-007, TASK-008, TASK-009, then TASK-010.

| Task | Title | Status | Dependencies |
|---|---|---|---|
| TASK-006 | Extract the shared read-only completion assessment | Completed | None |
| TASK-007 | Add structured error details through the guidance catalog | Planned | TASK-006 |
| TASK-008 | Deliver local read-only sync inspection | Planned | TASK-007 |
| TASK-009 | Verify cross-surface parity and failure boundaries | Planned | TASK-008 |
| TASK-010 | Update consumer guidance and prepare Epic acceptance | Planned | TASK-009 |

Planning-review corrections are mandatory within these existing Tasks:
TASK-006 owns whole-read-path execution safety, positive unborn classification,
and preservation of the actual whole-docs marker scope. TASK-007 maps the new
inspection-only availability errors; TASK-008 binds strict readers and proves
no indirect effects. TASK-009 verifies S05, expanded S13, and new S20-S22;
TASK-010 explains the availability limit and safe consumer behavior.

Inspection conservatively stops before filter-capable reads when any effective
clean/process command is configured, including unused drivers. It does not
remove Continue's filter support. Only safely obtained verdicts must match;
policy-limited evaluation exits 1 with `inspection_unavailable`, never a guessed
clean/dirty result. Scratch index safety alone is insufficient. Reader fixes
must preserve valid inputs, not the existing concealment of Git failures.

Each implementation Task owns its focused tests and only promotes the contract
it actually delivers. TASK-009 verifies the combined failure boundaries;
TASK-010 performs the final consumer/documentation handoff and complete
repository gate. The dossier's SDI requirements and scenario matrix define
acceptance evidence. Completing the member Tasks does not automatically
complete the Epic; explicit acceptance and dossier closeout are still required.
This registration authorizes planning, not implementation, publication, or
installation.

## Standalone tasks

| Task | Title | Status | Dependencies |
|---|---|---|---|
| TASK-005 | Correct Sanho skill recovery guidance for rejected sync and push outcomes | Completed | None |

TASK-005 checks Aquarium's Low review findings R8-06, R8-13, and R8-14 against
current Sanho behavior. It clarifies the existing refresh authorization, the
distinct `sync --continue` refusals, and a failed application push after docs
publication. Acceptance requires source guidance consistent with native CLI
recovery, structural checks, and explicit reporting of unperformed agent
scenarios. The review IDs are external findings, not Sanho roadmap IDs.
