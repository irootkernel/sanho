# Changelog

## v0.2.0 - 2026-08-07

### Added before release (third external review wave)

- **`sanho sync --continue` completes a conflicted sync, and nothing else
  does.** Resolving stays ordinary git work — edit, `git add`, `git commit` —
  and the sync ends when you say it has. It clears the sync note and moves the
  docs base to the merge target; it creates no commit and opens no network
  connection, so it works offline. Until it runs, `git push` is refused and
  says which step is missing. This is a deliberate deviation from the "no new
  vocabulary" rule, recorded in `docs/architecture.md`: three waves of
  inferring completion from the state a resolution leaves behind each left a
  smaller path to the same data loss, and the fourth narrowing would have
  started rejecting genuine resolutions. It also opens the exit that was
  missing entirely — a resolution that keeps every conflicted path exactly as
  it was leaves no evidence at all, and can now simply be declared.

### Fixed before release (third external review wave)

- **Editing the conflicted file after a stash is no longer read as a
  resolution.** With the markers stashed away, continuing to work on the same
  document — the most natural next action — moved HEAD, moved the docs tree,
  and changed a path the merge conflicted on, which is everything the
  completion test asked for. The sync was declared finished and the next push
  republished pre-merge content over upstream at exit 0, while `sanho status`
  said "up to date". Hooks no longer complete or clear anything: the sync note
  is deleted by `sanho sync --continue` and `sanho sync --abort` alone, and
  every commit made inside the window says the sync is unfinished.
- **A commit made during a sync can no longer poison the base through its
  trailer.** The commit that resolved a sync used to be stamped with the merge
  target, and that trailer outlived the sync: after `sanho sync --abort` — the
  command the tool itself advises — one branch switch let base re-derivation
  adopt the target with pre-merge documents beneath it. Every commit inside the
  window now stamps the base file's own value, which is a true ancestor of what
  it carries, so the worst a later re-derivation can do is adopt a base that is
  too old.
- **`sanho sync --abort` over an unreadable note clears the docs base.**
  Skipping the base rested on "a conflicted sync never moves it", which is
  false after a crash between the base write and the note clear, and false for
  a note left by an older build. In both, the abort walked away from a base
  sitting on the merge target and the next push fast-forwarded over upstream.
  An abort that cannot read its note cannot know which value is right, so it
  takes the oldest one there is: none. Publication then refuses with `no_base`
  and names `sanho sync`, which establishes one.
- Every base write now obeys one invariant — a recorded base may never be ahead
  of the docs the worktree carries, and where neither can be established the
  older value wins. `sanho sync --continue` therefore clears the note before it
  writes the base (an interruption leaves the base too old, which publication
  reconciles); `sanho doctor --fix` and `sanho init` stand down while a sync is
  unfinished, as base re-derivation already did; and `sanho status` reports the
  unfinished sync instead of a "N behind — run 'sanho sync'" line that names a
  command refusing in that state.
- A sync note written by an older build is no longer explained with a reason
  nothing knows to be true ("no commit has changed the files it conflicted
  on" — it never recorded those files), and `--continue` completes it, where
  previously only an abort could end it.

### Fixed before release (second external review wave)

A re-review found that the first wave had hardened the test for "is this sync
resolved?" while leaving the dangerous state it was protecting: a conflicted
sync advanced the docs base to the merge target immediately, so for the whole
time a resolution was owed the workspace held `base == canonical head` with
pre-merge documents beneath it. Two paths still reached a publication from
there. The base advance now happens when a resolution is confirmed, which
removes the state rather than the two symptoms.

- **An unrelated docs commit no longer passes for a resolution.** With the
  markers stashed away, committing any other document moved HEAD and its docs
  tree — all the completion test asked for — so the sync note was cleared and
  the next push republished the pre-merge tree over upstream's work at exit 0.
  The sync note now records which paths the merge conflicted on, and a sync
  counts as resolved only once a commit has changed one of them. `pre-commit`
  says so for every unfinished sync whose markers are gone, not only for the
  tidy stashed case, and stays quiet when the commit being prepared is itself
  the resolution.
- **`sanho sync --abort` over a damaged note is lossless.** The previous base
  lived inside the note, so an abort that could not read it left the base on
  the merge target with pre-merge documents beneath it, and the next push
  fast-forwarded over upstream. A conflicted sync no longer moves the base, so
  there is nothing for that abort to restore: it succeeds, says only that the
  docs were restored, and the follow-up `sanho doctor --fix` line is gone. An
  abort still restores the base recorded in a readable note, which is what a
  workspace left mid-sync across this upgrade needs.
- Consequences of holding the base still, all local and none of them networked:
  the resolution commit stamps the merge target it derives from (and a commit
  that leaves the conflicted paths alone stamps the pre-sync base, so a later
  checkout cannot re-derive its way back into the old state); base
  re-derivation and `sanho doctor`'s `base-derivation` check stand down while a
  sync owns the base; and the freshness warning is suppressed for the duration,
  since the notice above already describes that state and names the way out.
- The registry lock timeout is reported as `registry_lock_timeout` in the
  `--json` error envelope again, rather than as `internal`.

### Fixed before release (external review wave)

A second, external review of the v0.2 implementation found one critical defect,
four major ones, and a set of smaller ones. All are fixed on this release; none
shipped.

- **A stashed conflict is no longer mistaken for a resolved one.** A conflicted
  `sanho sync` was declared finished as soon as the docs were clean and no file
  carried a marker — a test that `git stash push -- docs` passes without
  resolving anything. The sync note was then cleared while the base had already
  advanced to the merge target, so the next push read it as a fast-forward and
  republished the pre-merge tree, reverting upstream's work with exit 0 and no
  message. The sync note now records where HEAD stood when the markers were
  written, and a sync counts as resolved only when a commit has moved HEAD and
  its docs tree. The put-aside state gets its own guidance: `pre-commit` reports
  it and does not block (it is not a reason to stop unrelated work), and
  `pre-push` refuses.
- **`sanho sync --abort` survives a damaged sync note.** A `sync.json` that did
  not parse made every path report the parse error, including the one operation
  whose contract is that it cannot fail once a note exists — leaving markers in
  `docs/`, a file nothing could read, and no command able to clear either. The
  note's existence and its readability are now separate facts: abort restores
  the docs and clears the note on existence alone, leaves the base where the
  sync put it (that value lived inside the note), and names
  `sanho doctor --fix`. `sanho sync`, `sanho pull`, `sanho doctor` and the push
  boundary route to the abort instead of printing the parse error, and
  `git commit` is never blocked by it.
- **The push marker gate is measured against canonical.** It scoped its scan to
  the diff against the *application remote's* previous tip, which one
  `git push --no-verify` poisons: the markers reach the code remote without ever
  passing the gate, and every later push then treats them as vetted history. The
  baseline is now canonical head's docs tree — a state every publication built by
  passing this same gate — so there is no way in behind it. An empty or
  unresolvable baseline still falls back to a full-tree scan.
- **A conflict rejection names the conflicted files.** Template 3 stated that a
  conflict existed and left the user to run `sanho sync` to find out where, while
  the rejection already carried the list.
- **`sanho doctor` reports a base that history disagrees with.** Base
  re-derivation is withheld whenever the docs worktree differs from HEAD's, and
  the resulting inconsistency was promised to be flagged and never was. The new
  `base-derivation` check warns when re-derivation would have produced a
  different base (and `--fix` writes it), reports `[info]` when the docs are
  dirty and the re-derivation was deliberately held back, and stays silent when
  the base is simply ahead of the stamped one — the ordinary state after a
  publication. `severity` gains `info`; `warnings` still counts only `warning`.
- Smaller: the sync-note refusal moved ahead of the canonical clone's creation,
  so the cheapest rejection no longer pays for a network round trip; `appgit:`
  and `gitx:` package tags no longer leak into `sanho doctor` output; the
  `--rebase-onto` empty-tree fallback and the automatic `state.json.v1.bak` are
  documented as they behave; and the JSON error envelope and the post-migration
  flow gained end-to-end coverage.

### Fixed before release (P7 review wave)

An adversarial review of the v0.2 implementation found six correctness defects
and a set of smaller ones. All are fixed on this release; none shipped.

- **A multi-ref push no longer deletes a branch's documents.** Publishing two
  branches with different docs trees in one `git push` (or one `git push --all`)
  decided each branch against the head the previous one had just moved, so the
  last branch's tree replaced the others wholesale — with exit 0 and no message.
  Publication now evaluates every pushed tip against one frozen canonical
  snapshot, chains the merges so a later tip is merged onto an earlier one
  rather than fast-forwarded past it, and writes nothing to canonical until the
  whole push validates. A push in which any tip conflicts is rejected whole, so
  "no remote ref was changed" is true by construction.
- **Concurrent merges no longer read each other's inputs.** The tree merge uses
  fixed ref names (they are what git writes into the conflict markers), and two
  merges in one ref store — the pre-commit preview against a concurrent push,
  or two linked worktrees — could publish a tree built from the other's content.
  The whole merge span now holds an exclusive lock on the shared git directory,
  and clears the refs a crashed merge left behind rather than failing on them.
- **A branch with no docs no longer empties the canonical repository.** Pushing
  a branch created before `docs/` existed published the empty tree over
  everything. It is refused, naming the branch and the number of documents at
  stake; `SANHO_ALLOW_DOCS_DELETION=1` performs the deletion when that is the
  intent.
- **Linked worktrees are managed.** `.sanho.json` is gitignored, so
  `git worktree add` produced a checkout where every hook silently did nothing:
  no marker gate, no provenance stamp, no publication. Configuration now
  resolves through the main worktree; the base file and the sync note stay
  per-worktree, and the registry keeps one row per checkout.
- **`sanho pull` no longer discards staged docs.** It compared only the worktree
  against the base, then overwrote the index. It now requires clean docs and
  says so in two steps, because `sanho sync` requires them too.
- **`sanho init --force` no longer discards uncommitted docs.** It refuses while
  the docs directory has changes that are in no commit.
- **The commit and push gates cost what the change costs.** They spawned two git
  processes per docs file in the whole tree — 39 seconds per commit at 4,000
  files. The pre-commit gate now scans the staged diff, the push gate scans what
  the push introduces, and both read every object through a single batched git
  process. A 500-file gate runs in tens of milliseconds.
- **`sanho migrate` is resumable and no longer destroys other projects'
  registrations.** It converts the whole v0.1 registry rather than lifting out
  one URL and letting the next write erase the rest, writes the v2 config last
  so an interrupted run leaves a workspace both versions can still act on, and
  backs up each hook file it rewrites. Ordinary commands refuse a v0.1 registry
  instead of overwriting it.
- **Guidance that named a command which fails where it is printed is gone.**
  The closure gate now scans every CLI file and the use-case error sentinels,
  not just the message catalog; `doctor` advises `sanho doctor --fix` and
  `sanho sync` where it used to advise the destructive `sanho init --force`.
- Smaller fixes: `--rebase-onto` refuses to adopt an ancestor of a healthy base;
  the docs checkout validates every path before deleting anything and names the
  recovery command on failure; large *binary* documents no longer fail the
  marker scan (only oversized text does); hooks refuse to follow symlinks and
  keep a user's comment-only hook file; a config with neither `schema_version`
  nor `socket_path` is reported as corrupt rather than as v0.1; `sync --abort`
  names untracked files it cannot remove; `--json` errors carry a machine
  envelope on stdout.

Sanho is now a single CLI. Publication moved from commit time to push time, the
daemon is gone, and the tool no longer creates commits in application
repositories.

### Added

- `sanho sync` reconciles local docs with canonical between your own commits:
  fetch, three-way merge, and one ordinary user-authored `docs: sync to <oid>`
  commit laid under your work. `--abort` restores the pre-sync state and cannot
  fail, because sync touches only the docs worktree and two state files.
  `--rebase-onto <commit>` reconciles against an explicit canonical commit for
  history-rewrite recovery.
- `sanho doctor` checks git, workspace config, hooks, the private clone, the
  base file, the registry lock, sync state, and the docs inventory. `--fix`
  re-derives a missing or invalid docs base from commit trailers, entirely
  offline. It exits 0 even when it reports warnings.
- `sanho migrate` converts a v0.1 workspace to the v0.2 layout in place. It is
  idempotent, refuses while a v0.1 transaction or pending-fix state exists, and
  prints the daemon stop command rather than running it.
- A per-workspace private bare clone of the docs repository at
  `<git-common-dir>/sanho/canonical`, with an explicit fetch policy and a
  recorded last-fetch time that every cached answer reports as data age.
- `--json` on `sync`, `pull`, and `doctor`, in addition to `status`, `state`,
  and `version`. A conflicted sync reports `status: "conflicts"` with exit 0.
- Bounded compare-and-swap retry on publication: a lost race refetches and
  re-decides from scratch, up to three attempts, never replaying a stale merge
  and never force-pushing.
- Guidance closure: every next-step command Sanho prints succeeds in the state
  where it is printed. When no command can succeed, the message says "manual
  intervention required" and gives diagnostics instead.
- A conflict-marker detector with no line-length limit, binary sniffing, an
  explicit oversize error, and error propagation that fails gates closed.

### Changed

- Publication happens in `pre-push`, not `pre-commit`. `git commit` is now local
  and network-free, and always works offline. A stale base produces one
  informational line instead of a designed failure and a retry.
- Provenance moved from identity to ancestry. Commits carry
  `docs-base: <commit>` and `docs-base-tree: <tree>` instead of
  `docs-version: <commit>`. The tree value is the anchor that survives a
  canonical history rewrite. Stamping is offline, covers amends and rewords, and
  never blocks a commit.
- Conflict resolution is the standard git idiom: `sanho sync`, edit, `git add`,
  `git commit`. There is no continue/abort/recover transaction protocol.
- Concurrency control moved from a single-machine in-process mutex to git's own
  push compare-and-swap, which works across processes and machines, plus a
  `flock` on `~/.sanho/state.lock` for the local registry.
- `.sanho_docs_hash` is replaced by `.sanho_base.json` (`{version, commit,
  tree}`). The legacy single-line file is still read as a compatibility input
  and is never written or deleted by v0.2.
- `.sanho.json` gains `schema_version: 2` and `docs_repo_url`, and drops
  `socket_path`. The CLI resolves the docs repository itself.
- `~/.sanho/state.json` is now a version 2 registry keyed by full repository
  URLs, updated directly by each CLI invocation under the lock. Basename-keyed
  collisions between different repositories with the same name are gone.
- Content moves between repositories as git objects over local transport, so
  symlinks and file modes are handled by git natively. The tar snapshot
  subsystem is not used on any path.
- Merges use `git merge-tree --write-tree` with conflict sides labeled
  `sanho-ours` and `sanho-upstream` instead of temp paths. Its exit contract
  (0 clean, 1 conflicted, higher a real error) is distinct from
  `git merge-file`'s and is read correctly.
- Hook installation and removal match by exact line for all six hooks, preserve
  foreign hook content verbatim, insert above a trailing `exit`, add only the
  owner-execute bit, and delete a file left holding nothing but a shebang.
- Every state write — registry, base file, workspace config, sync note, hook
  files — goes through one atomic writer with fsync on the file and its
  directory.
- All git invocations go through a single runner: argv-only, no shell,
  `GIT_TERMINAL_PROMPT=0` always, SSH `BatchMode` and a connect timeout on
  network operations, and per-command timeouts.
- No git version is enforced. `sanho init` does not gate on one and `sanho
  doctor` reports the detected version as information.
- `sanho status` reports base, canonical head with data age, the sync preview,
  sync-in-progress state, and siblings with relations computed locally. Offline
  status now answers from the last fetch with an explicit staleness line instead
  of refusing.
- `sanho clean --dry-run` is strictly read-only and does not touch the registry
  lock file.
- Documentation under `docs/` was rewritten for v0.2. `docs/architecture.md` is
  the implementation authority; `sanho-v0.2.md` is the historical design record.

### Removed

- The `sanhod` daemon, its Unix socket, and the entire HTTP surface — server,
  handlers, DTOs, and client. The `/healthz`, `/projects`, `/workspaces`,
  `/docs/head`, `/docs/snapshot`, `/docs/push`, and `/state` endpoints no longer
  exist, and neither do the launchd and systemd deployment paths.
- Tool-authored `[SANHO] Update docs` commits. Sanho never creates a commit in
  an application repository.
- The pull-commit engine, its five-phase transaction, recovery classifier, and
  recovery refs, along with `sanho pull-commit` and its `--continue`,
  `--abort`, and `--recover` modes.
- The application `main` publication contract and its store: Sanho no longer
  fast-forwards, pushes, or otherwise moves any application ref, and no longer
  blocks branch pushes through alias remotes or direct URLs.
- The dirty-layer capture, throwaway-clone rebase, and pulled-docs baseline
  machinery. `sanho sync` requires clean docs paths instead.
- `sanho fix`, `.sanho_pending_fix` handling, the `post-commit` hook, and the
  `workspace register`/`unregister` commands.
- Git operation detection. Sanho no longer inspects rebase, merge, cherry-pick,
  revert, bisect, or sequencer metadata, and no longer blocks or reports on it.
- The `--socket` global flag, the `SANHO_SOCKET` environment variable, and
  `socket_path` in the workspace config.
- The `{"error":{"code":…}}` JSON error document and its machine error codes.
  Errors are English text on stderr; stdout under `--json` is either a complete
  document or nothing.
- The daemon and client Make targets, and the daemon integration and end-to-end
  suites.

### Compatibility and migration

- **The canonical docs repository is untouched.** Same repository, same linear
  main history. Only the commit-message convention changes going forward.
- Legacy `docs-version:` trailers and `[SANHO] Update docs` commits remain valid
  history. Every path that scans history accepts both trailer keys, so mixed
  histories need no rewrite and no migration commits.
- Installing the v0.2 binary routes v0.1-era hooks to it immediately, because
  hook lines invoke `sanho` by name. v0.2 degrades safely: `pre-commit`,
  `commit-msg`, and the `post-*` hooks print one migrate hint and exit 0, so
  commits keep working throughout the transition, while `pre-push` fails closed.
  No commit is ever blocked by the upgrade.
- Per machine, per workspace: finish any v0.1 pull-commit transaction with the
  v0.1 binary, install v0.2, then run `sanho migrate` in each workspace.
  `sanho migrate` writes `.sanho.json.bak` and `.sanho_docs_hash.bak` and leaves
  the legacy hash file in place. It rewrites `~/.sanho/state.json` and its `.bak`
  into the v2 schema, and copies the daemon-era file to
  `~/.sanho/state.json.v1.bak` before doing so — that copy is what later
  workspaces read the project-to-docs-repo mapping from once `state.json` itself
  is v2, so it is made automatically and no manual backup is required.
- Stopping and unloading the v0.1 daemon remains the user's action. `sanho
  migrate` prints the exact `launchctl bootout` or `systemctl --user disable
  --now` line and never runs it. The `sanhod` binary can be deleted at leisure.
- Mixed-version operation in one workspace is not supported and is mechanically
  prevented. Different machines may temporarily run different versions against
  the same canonical repository — both versions' publications serialize through
  git — but that is a transition state, not a supported configuration. Migrate
  one machine at a time.
- Automation that branched on the v0.1 error codes, on `pull_commit`,
  `main_publication`, `head_reconciliation`, or `git_operation` status fields,
  or on `sanho pull-commit` and `sanho fix` must be updated. The v0.2 stable
  vocabulary is documented in `docs/cli-json.md`.
- `sanho version --json` keeps its v0.1 schema, so existing version checks
  continue to work.
- Supported operating systems are unchanged: macOS and Linux.

## v0.1.6 - 2026-08-05

### Added

- `git_operation` JSON now distinguishes active rebase backends from orphaned
  metadata and reports exact metadata paths, a verified OID when available,
  and a recovery classification.
- `head_reconciliation` reports a valid reachable HEAD that awaits local
  docs-hash reconciliation without misclassifying it as canonical drift.
- A supported recovery guide covers conditional orphan pseudo-ref deletion and
  repair of an already-published branch with invalid docs provenance.

### Fixed

- A standalone `REBASE_HEAD` no longer impersonates an active rebase or lets a
  normal commit silently skip Sanho provenance handling.
- Pre-push validates every proposed non-delete branch OID before any main or
  target ref is published, rejecting missing, forged, unknown, unreachable, or
  tree-mismatched `docs-version` provenance without partial multi-ref updates.
- Lifecycle hooks never mutate Sanho state while a real Git operation is
  active. Pre-commit and pre-push reconcile a valid HEAD after the operation
  clears, including fast-forward and no-op rebases without post-rewrite.
- Legacy pre-push hooks are upgraded with an atomic same-directory replacement
  that preserves custom content and permission bits while ensuring the hook is
  executable, so remote arguments are never executed as shell commands by the
  running hook. Linked worktrees install hooks in Git's shared hooks directory.

### Compatibility

- Workspace configuration, daemon HTTP APIs, and transaction formats are
  unchanged. The `git_operation` fields and `head_reconciliation` object are
  additive JSON changes.
- Tag-only pushes and branch deletions retain their existing behavior. Existing
  legacy hooks may require one explicitly reported retry after atomic upgrade.

## v0.1.5 - 2026-08-04

### Added

- The root README and Korean guide now provide one shared, copy-ready Sanho
  workflow for `AGENTS.md` and `CLAUDE.md`.
- The AI agent guidance requires structured status checks, normal Git hooks,
  explicit authorization for destructive Sanho commands, and user confirmation
  before choosing Git recovery actions.

### Compatibility

- This release changes documentation only. CLI, daemon, HTTP, JSON,
  configuration, transaction, and Git hook contracts remain unchanged.
- Existing workspaces require no reinitialization or state migration.

## v0.1.4 - 2026-08-04

### Added

- `sanho status` and `sanho status --json` now expose worktree-aware Git
  operation state, classification, and operation-specific recovery choices.

### Fixed

- Workspace mutations now fail closed while a user-owned rebase, merge,
  cherry-pick, revert, bisect, mail apply, or sequencer operation is active or
  stale, even when HEAD matches origin/main and the index and worktree are
  clean.
- Git operation metadata is resolved through Git for linked worktrees and is
  never automatically aborted, quit, or deleted by Sanho.
- Commit lifecycle hooks skip Sanho-owned mutations while Git recovery is in
  progress, while pre-push continues to block remote publication.
- Commit replay during a rebase now reports rebase-specific recovery commands
  instead of misclassifying Git's transient cherry-pick marker as a second
  operation.
- A successful rebase `post-rewrite` now validates Git's old/new commit
  mappings and reconciles only Sanho transaction and workspace metadata even
  though Git has not removed its rebase marker yet.
- Large `post-rewrite` mapping sets now validate commit reachability in one
  batched Git process under a dedicated reconciliation timeout, avoiding
  timeouts caused by one ancestry process per rewritten commit.
- Post-rewrite reconciliation now consumes a worktree-, HEAD-, command-, and
  mapping-bound validation permit instead of resolving every rewritten commit
  tree in a separate Git process. Inactive amend rewrites receive the same
  batched object and reachability validation.
- `post-rewrite` mapping lines now accept optional trailing Git metadata while
  continuing to validate the first two full commit IDs and trusted input
  provenance.
- Status inspection no longer refreshes the application workspace's
  remote-tracking refs or removes its main publication metadata. The daemon may
  still refresh its managed canonical docs clone while answering project status.

### Compatibility

- Existing workspace configuration, daemon APIs, pull-commit transactions,
  main publication state, and JSON fields remain compatible. The
  `git_operation` status object is additive.
- A Git operation must be completed, aborted, or quit before explicit Sanho
  mutation commands and pre-push can proceed. No workspace reinitialization is
  required.

## v0.1.3 - 2026-08-03

### Added

- A hands-on release checklist now covers real remotes, branch rules, legacy
  recovery, linked worktrees, network failures, and service upgrades.
- Origin branch pushes publish pending docs system commits through the existing
  Git pre-push hook: direct main pushes keep their full history, while other
  branches publish the full local main branch first.
- `sanho status` and `sanho status --json` report pending, blocked, or corrupt
  application main publication state.
- `sanho pull-commit --recover` preserves backup refs for HEAD, the index, and
  the worktree before reconciling an interrupted transaction.
- `sanho status` and `sanho status --json` report active pull-commit phase,
  classification, and the exact safe next command.

### Fixed

- Pending main publication now blocks branch pushes through alias remotes and
  direct URLs until origin/main is published, while leaving tags and deletions
  unaffected.
- Legacy version 1 and 2 pull-commit recovery now validates the recorded merged
  docs snapshot before clearing a sibling rewrite, preserving ambiguous or
  corrupt transactions and their recovery refs.
- Main publication is fast-forward only, survives target-branch push failures,
  and retries through the same `git push` without force-pushing.
- `git commit --amend` and repeated amend operations now reconcile prepared
  pull-commit transactions through Git's `post-rewrite` mappings.
- Post-commit retry, post-rewrite, recovery, and stale-state cleanup are
  idempotent, while ambiguous transactions continue to block pushes.

### Compatibility

- Existing workspace configuration, daemon state, CLI/HTTP/JSON contracts, and
  version 3 pull-commit transactions remain compatible without reinitializing
  workspaces.
- Version 1 and 2 pull-commit transactions require their recorded merged-index
  snapshot for safe sibling-rewrite recovery; unverifiable state remains
  preserved and push-blocking.

## v0.1.2 - 2026-07-29

### Fixed

- `pull-commit` now resolves trivial three-way file outcomes before invoking the
  text merge driver, so unchanged and one-sided binary files no longer block
  documentation materialization.
- Divergent binary changes fail closed without changing HEAD, the index, the
  worktree, the local docs hash, or pull-commit transaction state.

### Compatibility

- Existing configuration, daemon state, Git hooks, and CLI/HTTP contracts remain
  compatible and do not require workspace reinitialization.

## v0.1.1 - 2026-07-29

### Fixed

- Runtime cleanup and response-writing failures are now handled explicitly.

### Changed

- Standardized Sanho-owned component terminology on `daemon` across
  documentation, CLI output, errors, internal identifiers, and tests.
- Domain and use-case boundaries are enforced by an architecture guardrail.
- `golangci-lint` v2.11.3 is pinned as a reproducible Go tool dependency.
- `make test` runs prepare, unit, integration, and end-to-end phases in order,
  with separate daemon and client targets.

### Compatibility

- Existing `.sanho.json` files, Git hooks, Unix socket paths, and daemon runtime
  state remain compatible and do not require workspace reinitialization.
- The machine-readable CLI error code `server_request_failed` is now
  `daemon_request_failed`. Automation that matches the previous code must
  update; no compatibility alias is provided.

## v0.1.0 - 2026-07-29

First public Sanho release.

### Added

- `sanho` CLI and `sanhod` daemon installable with `go install`.
- Unix-socket-only local HTTP transport.
- Private runtime state and managed docs clones under `~/.sanho/`.
- Foreground daemon lifecycle with graceful SIGINT and SIGTERM shutdown.
- User-managed launchd and systemd deployment guidance.
- Unit, integration, and end-to-end coverage for Unix socket workflows.

### Changed

- Go module path is `github.com/irootkernel/sanho`.
- Workspace configuration uses `.sanho.json` with an absolute `socket_path`.
- Workspace metadata, Git hooks, commit messages, and environment variables use
  Sanho identifiers.

### Compatibility

This is a clean-break release. It does not read or migrate Kkachi configuration,
runtime state, service registrations, or workspace metadata. Supported operating
systems are macOS and Linux.
