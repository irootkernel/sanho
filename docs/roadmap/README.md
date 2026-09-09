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
| [EPIC-002](#epic-002-integrate-sanho-with-the-aquarium-development-channel) | Integrate Sanho with the Aquarium development channel | In Progress |

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

- Status: In Progress
- Objective: Build Sanho from an exact local commit, integrate its foreground
  executable with `~/.aquarium-dev`, and return verified candidate evidence to
  Aquarium while preserving production installation and native Git behavior.
- Detailed SOT: [Aquarium development integration](../todo/TODO-AQUARIUM-DEV.md)
- Dependencies: Aquarium's existing foreground producer contract and native
  development manager. EPIC-001 remains completed and is not reopened.
- External mapping: Aquarium EPIC-002 / TASK-014 owns consumer integration
  acceptance; Aquarium TASK-015 owns final cross-project cold validation.
  Those identifiers belong to Aquarium's separate roadmap namespace.

| Task | Title | Status | Dependencies |
|---|---|---|---|
| TASK-002 | Implement the Aquarium foreground producer and build identity | Completed | None |
| TASK-003 | Verify exact-commit builds and publication failure boundaries | Completed | TASK-002 |
| TASK-004 | Integrate the canonical checkout and hand off the verified candidate | In Progress | TASK-003 |

Epic acceptance requires the producer, isolated verification, approved host
integration, and return handoff described in the dossier. Child completion does
not close the Epic automatically or declare Aquarium TASK-014 complete.
