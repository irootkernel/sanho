# Deferred Feedback

This index owns small actionable findings intentionally postponed from current
work.

## Shared object verification cost

- **Owner:** `internal/infra/appgit` Git readers.
- **Impact:** Full blob verification preserves trustworthy read results, but
  hooks and interactive commands also pay a cost proportional to docs content.
  The existing 1,000-file, 52 MB scale scenario passes; per-command overhead
  has not been measured.
- **Re-entry:** When larger docs repositories or reported hook/command latency
  justify investigation, benchmark the affected readers and their callers.
  Use those measurements before proposing a narrower verification boundary;
  preserve missing/corrupt-object detection wherever a verdict depends on it.

Each entry must identify the finding, owner, reason for deferral, and concrete
next action. Work required for current correctness or acceptance cannot be
deferred. Promote epic-sized work to a TODO candidate or an adopted roadmap
unit instead of recording it here.
