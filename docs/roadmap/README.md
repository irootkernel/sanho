# Sanho Roadmap

This file is the sole owner of adopted Epic and Task identity, ordering,
dependencies, lifecycle vocabulary, and current status for the Sanho delivery
scope. No roadmap units are currently adopted.

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
