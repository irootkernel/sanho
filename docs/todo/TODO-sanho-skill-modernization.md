# Sanho skill modernization for GPT-6 Astra

This dossier defines execution requirements for EPIC-001 and TASK-001. The
[roadmap](../roadmap/README.md#epic-001-sanho-skill-modernization-for-gpt-6-astra)
alone owns their identity, dependencies, and lifecycle. Aquarium's accepted
`SKILL-07` requirements map to this work.

## Goal and delivery boundary

Make the source-distributed skill load only the guidance needed for the current
request. Clarify evidence freshness, authorization, recovery, and completion so
GPT-6 Astra can continue authorized work without repeated investigation or
unsupported success claims.

Deliver the changes as one Task: the entrypoint, its references, distribution
inventory, and verification must agree. Edit the source under
[`skills/use-sanho/`](../../skills/use-sanho/), the affected resource entries in
the [Makefile](../../Makefile), and directly related public documentation.
Installed skill copies are comparison evidence, not edit targets.

The [architecture](../architecture.md) owns runtime and safety contracts;
[CLI JSON](../cli-json.md) owns machine interfaces. Consult the relevant
implementation and tests when correcting descriptions. The [root README](../../README.md)
owns public workflows, and [deployment](../deployment.md) owns distribution
guidance. Reconcile discrepancies with these authorities before changing prose.

## Required changes

- Shorten the description around capability and activation boundaries. Keep
  ordinary commit and push guidance, essential safeguards, and completion
  conditions in the entrypoint. Do not impose a character quota.
- Move detailed diff, log, show, provenance filtering, and preview guidance to
  references selected by the request or current Git boundary. Give each moved
  requirement one owner and a reachable loading condition. Keep safeguards
  available before the actions they govern and reuse existing references where
  they fit.
- Distinguish reusable environment facts from mutable Git and Sanho evidence.
  Preserve local status checks before commits, refreshed canonical checks before
  pushes, and appropriate verification after mutations. Cached remote evidence
  must remain identified as cached.
- Select recovery diagnostics for the failure being handled. Define when preview
  is useful without making preview or full diagnosis mandatory for every push
  or failure. Preserve complete CLI-advised recovery sequences.
- Carry existing authorization through reconciliation and recovery for the same
  target and effects. Preserve separate authorization for materially different
  effects, including initialization, destructive recovery, and publication.
  Synchronization that creates a commit must retain Git authorization.
- Determine commit success from the actual Git result and relevant state. A
  pre-commit freshness warning alone proves neither failure nor success; never
  repeat a successful commit because of that warning. Reconcile uncertain
  outcomes before retrying.
- Correct directly related README claims about Sanho creating sync commits and
  the commit hook blocking conflict markers. Update affected links, download
  instructions, and required resource inventories together.

Preserve command grammar, JSON schemas, identity and revision checks,
idempotency, evidence freshness, and native lifecycle behavior. Preserve
unrelated work. Change executable helpers only when a changed resource layout
or an explicitly scoped integration contract requires it; this Task does not
introduce product runtime features or an evaluation framework.

## Verification and acceptance

Run `make docs-check`, frontmatter validation, reference reachability and
distribution completeness checks, and `git diff --check`. Apply one final
English Humanizer pass while preserving technical meaning. Add executable tests
only when helper behavior changes; select the relevant repository gates for
that change. Do not add prose-matching tests or automated LLM evaluations.

Prepare the following manual scenarios for Master using isolated fixtures where
mutations are needed. Exercise each only within its applicable authorization.

| Scenario | Required observation |
|---|---|
| Routine editing, review, build, or test | Sanho is not invoked without a relevant Git boundary or explicit Sanho request. |
| Authorized commit and push | Commit uses local evidence; push uses refreshed canonical evidence. Status and preview never grant permission. |
| Inspection and history questions | Only relevant references load; provenance, filtering, binary content, and cached-state contracts remain correct. |
| Freshness warning and successful commit | The warning does not trigger a duplicate commit or unauthorized sync. |
| Freshness warning followed by Git commit failure | The agent reports the actual failure and does not claim success from the warning. |
| Authorized reconciliation and rejected push | The agent follows the complete recovery sequence and continues the same authorized operation without redundant approval. |
| Changed targets or effects | The agent checks whether existing authorization still covers the action and requests additional authority when required. |
| Interrupted mutation or manual recovery | The agent establishes the outcome before retrying and preserves explicit recovery decisions and safety guards. |

Report structural and executable checks separately from observed Astra behavior.
Unperformed manual checks remain explicit acceptance gaps; structural success
does not establish functional completion. Make no token, latency, or runtime
performance claims without measurement. Completing the Task does not by itself
complete the Epic.

## Aquarium integration and handoff

Aquarium owns guidance for projects established to have no Sanho configuration,
including suppression of routine skill loading and commands, explicit user
requests to adopt or diagnose Sanho, and guidance updates after adoption.
Its planned destinations are Aquarium `TASK-045` for `dev-setup` and affected
callers such as `task-commit`, and Aquarium `TASK-046` for integration scenarios.
These are external task identities, not members or prerequisites of this Epic.
Sanho must not implement those Aquarium changes in this Task.

Aquarium `TASK-046` will inspect the resulting local `skills/use-sanho/` tree and
linked references. Provide the changed source locations, revision and relevant
uncommitted content, preserved contracts, verification results, manual gaps, and
separate commit, release, installation, and activation state. HEAD alone does
not identify uncommitted source. Release or installation is not an intake
prerequisite. Update this handoff if its consumer or source contract changes.

Keep detailed runtime evidence in its native system. Retain evidence in tracked
documentation only when a named downstream consumer requires a reviewed
promotion under repository policy. Registration does not authorize staging,
commits, publication, installation, or Mulgae review.

At Epic closeout, promote durable guidance to its canonical owners, replace the
roadmap's Detailed SOT with Canonical Outcomes links, and remove this dossier
and its TODO index entry under the repository's closeout rules. Preserve any
external instruction that Aquarium still needs in the appropriate owner.
