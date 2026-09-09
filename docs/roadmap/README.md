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
| [EPIC-001](#epic-001-sanho-skill-modernization-for-gpt-6-astra) | Sanho skill modernization for GPT-6 Astra | Planned |

## EPIC-001: Sanho skill modernization for GPT-6 Astra

- Status: Planned
- Objective: Make the source-distributed Sanho skill concise and conditional
  while preserving current Git, synchronization, and authorization contracts.
- Detailed SOT: [Sanho skill modernization](../todo/TODO-sanho-skill-modernization.md)
- Dependencies: None. Aquarium integration consumes the resulting local source
  and does not block this Epic.
- External mapping: Aquarium's proposal label `SKILL-07` maps to this Epic and
  its single member Task. It is not a Sanho roadmap identifier.

| Task | Title | Status | Dependencies |
|---|---|---|---|
| TASK-001 | Restructure and verify the Sanho skill for GPT-6 Astra | Planned | None |

TASK-001 delivers the skill, affected references and distribution guidance,
directly related documentation corrections, and verification defined in the
Detailed SOT as one coherent change. Epic acceptance remains separate from Task
completion and requires Master's applicable manual verification.
