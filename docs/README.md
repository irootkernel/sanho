# Sanho Documentation

This index establishes documentation ownership for the single Sanho delivery
scope. It adopts the repository's established document paths without moving or
renaming them.

- Profile: `legacy-adopt`
- Delivery scope: `sanho` at `docs/`
- Canonical roadmap: [`roadmap/README.md`](roadmap/README.md)

The root [`README.md`](../README.md) is the public product entrypoint. The
`docs/` tree is for maintainers and contributors who need implementation
contracts, operational procedures, delivery state, and verification guidance.

## Canonical roles

| Role | Owner | Responsibility |
|---|---|---|
| Specifications | [Specifications](specs/README.md) | Required behavior and durable public contracts |
| Architecture | [Architecture](architecture/README.md) | Current components, boundaries, data flow, and safety invariants |
| Architecture decisions | [Architecture decision records](architecture-decision-records/README.md) | Accepted and superseded structural decisions with rationale |
| Implementation tips | [Implementation tips](implementation-tips/README.md) | Non-normative development, validation, and release guidance |
| Operations | [Operations](ops/README.md) | Installation, operation, diagnosis, recovery, and removal |
| Roadmap | [Roadmap](roadmap/README.md) | Work identity, ordering, dependencies, lifecycle, and status |
| TODO | [TODO](todo/README.md) | Future epic-sized candidates and active Epic dossiers |
| Deferred feedback | [Deferred feedback](deferred-feedback/README.md) | Small actionable findings postponed from current work |

Supplementary release history lives in [`CHANGELOG.md`](../CHANGELOG.md) and
Git history. Those records explain how the product reached its current state;
they do not override current product or implementation authority.

## Authority and precedence

Use the narrowest current authority for the question:

- the root `README.md` for the product boundary and public workflows;
- [`cli-json.md`](cli-json.md) for machine-readable CLI contracts;
- [`architecture.md`](architecture.md) for runtime and implementation
  contracts;
- [`operations.md`](operations.md), [`deployment.md`](deployment.md), and
  [`recovery.md`](recovery.md) for their respective operational procedures;
- [`hands-on-testing.md`](hands-on-testing.md) for real-environment release
  verification;
- this index and the role indexes for ownership and routing only.

If authorities disagree, report and reconcile the mismatch instead of choosing
one silently. Do not use release history as current product truth.

## Roadmap identity

This repository has one roadmap namespace at [`roadmap/README.md`](roadmap/README.md).
Epic and Task identities are allocated independently and are never reused. The
roadmap alone owns lifecycle status; TODO dossiers provide delivery detail but
do not become a second status authority.

## Language and validation

Repository documentation is written in English. Run the repository-owned
documentation check with:

```bash
make docs-check
```

Aquarium documentation setup may additionally run its bundled structural
inspector. Structural success does not prove that documentation matches the
implementation or runtime behavior.
