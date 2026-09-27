# Specifications

This index owns required behavior and durable product contracts for Sanho.

- [`../cli-json.md`](../cli-json.md) is the canonical machine-readable CLI
  contract. Its [planned structured sync diagnostics](../cli-json.md#planned-structured-sync-diagnostics)
  section owns EPIC-003's adopted target interface and remains explicitly
  separate from implemented behavior until the corresponding delivery.
- [`../architecture.md`](../architecture.md) owns the current runtime and
  implementation invariants that constrain behavior.
- The root [`README.md`](../../README.md) owns the public product boundary and
  supported workflows.

Add a separate specification only when a durable behavioral contract does not
belong to one of those existing authorities. Do not copy implementation detail
or release history into this role.
