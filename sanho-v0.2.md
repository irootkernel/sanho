# Sanho v0.2 — Design Specification

**Status**: Accepted (design review completed 2026-08-07)
**STATUS — implemented**: this design was implemented on branch `v0.2` on 2026-08-07. Per the "Relationship" paragraph below, `docs/architecture.md` has been rewritten to match the shipped code and is now the implementation authority; this file is the historical design record. Where the two disagree, the code is correct and `docs/architecture.md` states the outcome — including the points §11 left open and the places where implementation refined the design (notably: `--json` also on `pull` and `doctor`, and `sanho migrate` having no `--purge` flag and rewriting `~/.sanho/state.json` in place).

**STATUS — amended 2026-08-07 (P7 fix wave)**: an adversarial review of the shipped code found six correctness defects this design did not anticipate, and the fixes changed three contracts stated above. They are recorded here so this file does not read as if it still described the code:

- **§5.3 publication is evaluate-then-publish.** The design describes a per-tip loop; a multi-ref push decided each tip against the head its predecessor had just moved, so the second tip's tree fast-forwarded over the first and deleted its documents with exit 0. Publication now decides every tip against one frozen snapshot, chains the merges, and writes nothing until the whole push validates.
- **§5.3 gained an empty-docs refusal.** A tip whose docs tree is empty, pushed at a canonical that has documents, is refused rather than published; `SANHO_ALLOW_DOCS_DELETION=1` states the deletion explicitly.
- **§5.4 merges hold a lock.** The fixed marker-label refs are safe only under an exclusive flock on the shared git directory; the design assumed one merge at a time per repository, which linked worktrees and the pre-commit preview both violate.
- **§5.2 covers linked worktrees.** `.sanho.json` is gitignored, so a linked worktree has none and every hook there was silently inert. Config resolution now falls back to the main worktree; the base file and sync note stay per-worktree.
- **§5.6/§5.3 marker gates are diff-scoped**, which is both a semantic correction (the gate is about what you are committing or pushing) and the fix for a cost that reached 39 seconds per commit at 4,000 docs files.
- **§5.8 `--json` errors carry a machine envelope** (`{"error":{"code","message"}}`) on stdout. The line above previously recorded "no user-visible machine error codes"; that is no longer true, and `docs/cli-json.md` documents the vocabulary.

**STATUS — amended 2026-08-07 (third review wave)**: completing a conflicted sync is now an explicit act, `sanho sync --continue`, which is a deliberate deviation from D3's "no new vocabulary (resolve → add → commit)". Three waves of narrowing the after-the-fact predicate for "was this merge resolved?" each left a smaller door into the same data loss — the last reproduction stashes the markers, keeps editing the conflicted file, and commits, which satisfies every question tree evidence can ask while the merge stands untouched — and a predicate narrow enough to reject it starts rejecting genuine resolutions. The new verb is isomorphic to `git rebase --continue`, so the learning cost is one line in the conflict template; the alternative was losing upstream's work. It is paired with one invariant on every base write ("a recorded base may never be ahead of the docs the worktree carries; where neither can be established, the older value wins"). `docs/architecture.md` states both.
**Audience**: the Sanho development team. This document is the single source of truth for the v0.2 architecture. It is written so that implementation can proceed without further design decisions except where §11 explicitly defers them.
**Relationship to `docs/architecture.md`**: that document describes the v0.1.x implementation and remains authoritative for the code currently in `main`. This document describes the v0.2 target. When v0.2 lands, `docs/architecture.md` must be rewritten to match it and this file becomes historical.

---

## 1. Overview

Sanho keeps the `docs/` directories of multiple application repositories synchronized with one canonical docs repository, and detects divergence early. v0.2 is a re-architecture driven by a full design/implementation/practicality audit performed on 2026-08-07. It preserves what the audit confirmed works — the working-copy model, early conflict detection, durable provenance, zero-data-loss behavior — and removes the structural causes of every Critical finding.

### The four principles

Every v0.2 design decision follows from these. When in doubt during implementation, resolve in favor of the principle.

| # | Principle | One-line consequence |
|---|---|---|
| P1 | **Publish on push.** Canonical publication happens at `git push` time, never at commit time. | Commits are local and private, exactly as in plain git. |
| P2 | **Detect on commit.** The commit path performs a read-only freshness check that never touches the network and never blocks. | `git commit` always works offline. |
| P3 | **The tool never authors commits in the application repository.** All commits are made by the user (or their agent) with ordinary git commands. | No `[SANHO] Update docs` commits, no ref manipulation, no transaction engine. |
| P4 | **No daemon.** Sanho is a single CLI. Coordination uses git's own push semantics; local shared state uses a file lock. | Nothing to install, start, supervise, or lose. Works in CI and containers. |

### What changes at a glance

| | v0.1.x | v0.2 |
|---|---|---|
| Publication trigger | `pre-commit` hook (every commit) | `pre-push` hook (every push) |
| Offline `git commit` | **Blocked** (daemon round-trips precede docs check) | Always works |
| Stale base at commit | First commit **fails by design**; retry required | One-line warning; commit succeeds |
| Conflict resolution | 5-phase pull-commit transaction, `--continue/--abort/--recover` | `sanho sync` → resolve → `git add` → `git commit` (standard git idiom) |
| Tool-created commits | `[SANHO] Update docs` (43% of history in audit sandbox) | None, ever |
| Provenance trailer | `docs-version:` (identity: "my tree == canonical X") | `docs-base:` + `docs-base-tree:` (ancestry: "my edits derive from X") |
| Concurrency control | Daemon in-process mutex (single machine) | git push compare-and-swap (any machine) + flock for the local registry |
| Components | `sanhod` daemon + `sanho` CLI + 7 hooks | `sanho` CLI + 6 hooks |
| Runtime dependencies | cobra | cobra (unchanged) |
| Git version | unspecified | not enforced — uses the installed git; merge paths rely on `git merge-tree --write-tree` (git ≥ 2.38 in practice; see §5.4) |

---

## 2. Background: why v0.2

A three-stage audit (static exploration → adversarial verification of 14 claims → empirical sandbox reproduction of 10 scenarios → three independent deep reviews) produced 26 confirmed findings: 3 Critical, 5 High, 10 Medium, 8 Low. Grades: design C, implementation C+, practicality today D+, **data safety A-** (no user data was lost in any trap explored — this property must be preserved).

The three Criticals:

1. **C1 — Offline lockout.** The pre-commit hook performed two unconditional daemon round-trips *before* checking whether the commit touches docs at all. With the daemon down (reboot, laptop, container), `git commit` failed for every commit, including code-only commits. Undocumented anywhere.
2. **C2 — Multi-hunk merge wedge.** `git merge-file` returns the *number of conflicts* as its exit code; the code treated exit > 1 as a hard error. Any file with ≥ 2 conflicting hunks — the most ordinary form of a two-person docs conflict — produced a repeatable, guidance-free failure with no transaction to recover from.
3. **C3 — Permanent transaction wedge.** After canonical history rewrite, a `SyncCommitted`-phase transaction could reach a state where `--abort`, `--recover`, `--continue`, and `clean` all provably fail, while the tool's own guidance pointed at the command proven to fail.

The root-cause diagnosis that motivates v0.2's shape: **commit-time publication was never a product requirement — it was forced by the provenance mechanism.** The `docs-version:` trailer must be written into the commit message, so the canonical OID had to exist before `commit-msg` ran, so publication had to happen in `pre-commit`. Everything downstream — the daemon on the commit critical path, the designed-to-fail first commit, the transaction engine, the main-publication ordering contract, the `[SANHO]` commits — is scaffolding erected to make commit-time publication safe. v0.2 removes the forcing function (D2) and then the scaffolding (D1, D3, D4).

---

## 3. Decision records

### D1 — Publish on push, detect on commit

**Context.** Commit-time publication put the daemon on the critical path of every commit (C1), taxed every stale-base commit with a designed failure (audit M6), published unreviewed and even aborted commits to the shared canonical repo (M1, S7), and broke every tool that treats `git commit` exit ≠ 0 as failure (IDEs, scripts, CI — the audit's own harness broke this way).

**Decision.** Publication moves to `pre-push`. The commit path keeps a **read-only, network-free, fail-open** freshness warning. Push is the natural "shareable state" boundary in git; push rejection ("fetch first") is an idiom every git user already knows.

**Alternatives considered.**
- *Keep commit-time publication, fix ordering only*: removes C1 but keeps M1/M6, the transaction engine, and the retry tax. Rejected as treating symptoms.
- *Relocate the pull-commit engine to pre-push unchanged*: moves the complexity without reducing it; keeps tool-authored commits and the double-invocation tax at push. Rejected.
- *Queue-and-replay (per-commit snapshots replayed at push as individual canonical commits)*: preserves per-commit granularity in canonical, but nobody has articulated a need for intermediate WIP docs states in the SOT, and it adds a queue subsystem. Rejected; can be layered on later without re-architecture if a need appears.

**Consequences.** Canonical accumulates one commit per push instead of one per commit; traceability is preserved in the canonical commit message (§5.3). Conflict discovery moves from "hard stop at commit" to "warning at commit, enforcement at push" — the warning preserves ~90% of the early-detection value (D3 restores the rest via `sanho sync`). Feature-branch pushes still publish before PR review; a `publish_branches` policy knob is reserved for future team use (§10).

### D2 — Ancestry provenance: `docs-base:` + `docs-base-tree:`

**Context.** The v0.1 trailer records *identity* ("this commit's docs tree equals canonical commit X"). Under push-time publication that invariant cannot hold — a tip's docs tree legitimately does not exist in canonical until push. What publication actually needs is *ancestry*: which canonical state the local edits derive from (the merge base). Identity becomes derivable (after publish, canonical HEAD tree == tip docs tree by construction). Additionally, the audit showed the identity trailer was fragile: a message-only `git commit --amend -m` silently deleted it, and repair required a daemon round-trip that was itself blocked (H4).

**Decision.** New trailer pair, stamped by the `commit-msg` hook from purely local data:

```
docs-base: <canonical commit OID, 40- or 64-hex lowercase>
docs-base-tree: <that commit's docs tree OID, 40- or 64-hex lowercase>
```

The commit OID serves the everyday path (CAS comparison, behind-count computation, human cross-reference against canonical `git log`). The tree OID is the disaster anchor: if canonical history is rewritten and the commit OID becomes unreachable, the same content can be re-anchored by tree search (rewrites that preserve content — squash, rebase — preserve final trees). Both values live in the workspace base file (§5.7) and are stamped offline; loss by amend is repairable offline and is never a gate condition.

**Alternatives considered.**
- *Keep identity semantics*: logically incompatible with push-time publication (see Context). Rejected — not a style choice.
- *Commit OID only*: orphaned by history rewrite (this is exactly the C3 trigger). Rejected as sole record.
- *Tree OID only*: robust but human-illegible and requires history scans for ordering (behind N). Rejected as sole record.
- *No trailer (local state + git notes)*: notes don't propagate by default, are lost on rewrite unless configured, and vanish on fresh clones. The trailer's core value is that it travels with the repository. Rejected.

**Consequences.** Legacy `docs-version:` trailers remain valid history; the *key* distinguishes old from new commits mechanically, so mixed histories need no rewrite (§8). Trailers become a durable backup and a branch-switch recovery source (§5.10), not a gate input; the operational source of truth for publication is the base file.

### D3 — `sanho sync` and the no-tool-commits rule

**Context.** The v0.1 pull-commit engine (1,076 lines) exists because it runs *mid-commit*: it must capture staged/unstaged layers, inject a `[SANHO]` commit, rebase the layers in a throwaway clone, move `main` and the branch atomically, and survive interruption at five phases. That machinery produced the audit's worst traps (S8 chain, C3, M2). The deep reason is timing: a hook running mid-commit cannot ask the user for anything, and **mid-commit commit injection cannot avoid the fail-and-retry dance** — git's in-flight commit consumes the index, so the hook must rewrite the index and abort the user's commit to get a reliable result. This is inherent to git, not fixable by better engineering.

**Decision.** The tool never creates commits in the application repository (P3). All of today's automation moves into one explicit command that runs *between* commits:

```
sanho sync    # fetch canonical → 3-way merge → lay a base-update commit under your work
```

- Clean merge: sync writes the merged docs and creates one ordinary commit, `docs: sync to <short-OID>`, authored by the user. Subsequent user commits sit on a fresh base, so **their diffs contain only the user's own changes** (the diff-hygiene property of the old `[SANHO]` commits, preserved without hook magic).
- Conflict: sync materializes standard conflict markers in `docs/`, and the resolution is the standard git idiom — edit, `git add`, `git commit`. `sanho sync --abort` restores the pre-sync state and **cannot fail**, because sync touched only the docs worktree and one state file — no refs, no commits.
- The pre-commit warning tells the user (or agent) when sync is worth running and whether it would be clean (§5.6). Agents automate it by protocol (AGENTS.md: *on a behind warning, run `sanho sync`, then continue*), which reproduces v0.1's full automation for the agent persona without any mid-commit machinery.
- An opt-in `on_stale_base: auto-sync` mode (hook-time auto-sync accepting the retry dance) is deliberately **not** built in v0.2; add later only if real usage misses it.

**Guidance closure (normative).** Every next-step command Sanho prints must actually succeed in the state where it is printed, and this is enforced by tests (§9). If no command can succeed, Sanho prints "manual intervention required" plus diagnostics — never a command that will fail. (Audit H5 showed v0.1's guidance pointing at itself and at provably-failing commands.)

**Alternatives considered.** Keeping the engine (rejected: complexity and traps persist wherever it runs); server-side auto-merge only (rejected: conflicts need a place where human hands can resolve them); making sync implicit in pull (rejected: pull remains the consume-only, no-local-edits fast path — two verbs, two intents, §5.5).

### D4 — Daemonless

**Context.** With publication at push, the daemon's load-bearing role — serializing publication — is provided natively by git: a push to the canonical repo is compare-and-swap (non-fast-forward rejection), and unlike the in-process mutex it works across processes *and machines* (audit M3). What remains daemon-shaped is only the cross-workspace registry, whose substance is a JSON file plus access serialization — a job for `flock`, not a process. Audit findings H3 (startup hang, handler goroutine leak, no timeouts), M7/M8/M10 (HTTP/socket surface), and the launchd/SSH-environment failure class all attach to the process itself.

**Decision.** `sanhod` is removed. Its responsibilities map as follows:

| Daemon role (v0.1) | v0.2 mechanism |
|---|---|
| Publication CAS + per-repo serialization | `git push` non-fast-forward rejection, bounded retry (§5.3) |
| Canonical content serving (HEAD, snapshots) | Per-workspace private bare clone, `git fetch` (§5.2) |
| Workspace registry, cross-workspace status | `~/.sanho/state.json` guarded by `flock`, updated directly by each CLI invocation (§5.7) |
| Sibling relation computation (VS CURRENT / VS HEAD) | `git rev-list` in the local canonical clone — commit relations are clone-independent |
| Reported-hash validation | Not needed under ancestry semantics (this guard caused C3) |

Functional loss on a single machine: **none**. Sibling-info freshness is report-based in both architectures (identical characteristics; `last_updated_at` is displayed). Offline `status` *improves*: v0.1's daemon refused to serve stale state; v0.2 answers from the last fetch with an explicit staleness warning, because CAS correctness now lives at push.

**Alternatives considered.** Daemon as required mediator (rejected: availability class and onboarding cost remain); daemon as optional observer process (rejected after analysis: the observer role needs no process either — a locked file suffices); dual publication paths selected by config (rejected: two live implementations of the same function is the decay pattern the audit documented in A3.4 and the mock-hidden merge bug).

**Consequences.** `launchd`/`systemd` units, socket-path limits, HTTP hardening, and daemon lifecycle disappear from the product and its onboarding. Multi-machine sibling visibility does not exist — it never did (the daemon was per-machine); a future extension can carry workspace state as lightweight refs in the canonical repo (§10). If a long-lived process is ever wanted (team visibility server, watcher), it can be revived *on top of the same state file* without changing this design.

---

## 4. Target architecture

```
┌─────────────────────────── application repository ───────────────────────────┐
│                                                                              │
│  working tree                         .git/                                  │
│  ├── src/…                            ├── hooks/  (6 sanho lines, §5.10)     │
│  ├── docs/            ◄─ pull/sync ─┐ ├── sanho/                             │
│  ├── .sanho.json   (workspace cfg)  │ │   ├── canonical/   bare clone of the │
│  └── .sanho_base.json (base file)   │ │   │                docs repository   │
│                                     │ │   └── sync.json    (only during a    │
│  commits carry trailers:            │ │                     conflicted sync) │
│    docs-base: <oid>                 │ │                                      │
│    docs-base-tree: <oid>            └─┼── objects flow BOTH ways via         │
│                                       │   local git fetch (§5.2, §5.4)      │
└───────────────────────────────────────┼──────────────────────────────────────┘
                                        │
                         git push (CAS = non-fast-forward)
                                        │
                              ┌─────────▼─────────┐        ┌──────────────────┐
                              │  canonical docs   │        │ ~/.sanho/        │
                              │  repository       │        │  ├── state.json  │
                              │  (origin, linear  │        │  │   (registry,  │
                              │   main history)   │        │  │    flock'd)   │
                              └───────────────────┘        │  └── state.lock  │
                                                           └──────────────────┘
   sanho CLI (single binary)
   commands: init · status · state · sync · pull · clean · doctor · project · hook
   hooks:    pre-commit (detect, local-only) · commit-msg (stamp trailers)
             pre-push (publish) · post-checkout / post-merge / post-rewrite (re-derive base)
```

Data-flow summary:

- **Publish** (pre-push): app tip's docs tree → private clone (via local fetch) → commit on canonical `main` → `git push` to origin. Rejected pushes retry after refetch; conflicts route to `sanho sync`.
- **Consume** (`sanho pull` / `sanho sync`): fetch origin into private clone → merge/checkout into `docs/` → base file updated.
- **Observe** (`sanho status` / `state`): base file + private clone (relations) + `~/.sanho/state.json` (siblings).

---

## 5. Detailed specifications

### 5.1 Provenance

**Trailer format.** Exactly one of each key on a stamped commit, values matching `^(?:[0-9a-f]{40}|[0-9a-f]{64})$`:

```
docs-base: 67c4bbfeada37f5dda8fb79aa43216ef062cd8df
docs-base-tree: 2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6
```

Values are copied verbatim from the base file (§5.7) at `commit-msg` time. Stamping is **purely local** — no network, no clone access, no daemon. If the base file is missing or invalid, `commit-msg` prints a one-line warning and does not stamp; it never blocks the commit.

**Stamping rule.** Stamp when either condition holds (and the message does not already contain a `docs-base:` trailer):

1. the index's docs tree differs from `HEAD`'s docs tree (a commit that changes docs), or
2. `HEAD`'s docs tree differs from `HEAD~`'s docs tree (covers `--amend` of a docs-touching commit, including message-only rewords that wiped the previous trailer; when `HEAD~` does not exist, treat its docs tree as empty).

This rule over-stamps in one benign case (the first non-docs commit after a docs commit). That is acceptable by design: trailers are a durable record and a recovery source, **never a gate input**, so extra accurate trailers are harmless. This resolves audit H4: a reword-amend can drop the trailer, and the very next commit or a `sanho doctor --fix` restores it from local data.

**Semantics.** `docs-base` asserts: *the docs content of this commit derives from canonical commit `<oid>`* (i.e., that commit was the 3-way merge base for any local docs edits). It does **not** assert tree equality. Identity ("has this tip been published?") is not stored anywhere; it is computed when needed as `tip docs tree == canonical HEAD tree`.

**Legacy coexistence.** Commits carrying the v0.1 `docs-version:` key are read under their original identity semantics wherever history is scanned (see base re-derivation, §5.10): a `docs-version: X` commit's docs tree equaled canonical `X` at commit time, which means `X` is also a correct *base* for edits made on top of it. Therefore the re-derivation scan accepts both keys, preferring the newest reachable stamped commit. No history rewrite, no migration commits.

### 5.2 Canonical clone management

- **Location**: `<git-common-dir>/sanho/canonical` (bare). One per repository; linked worktrees share it (concurrent git access to a repo is protected by git's own ref locking). Resolved via `git rev-parse --git-common-dir`, so worktrees and submodules behave correctly.
- **Identity**: the clone's `origin` remote is the docs repository URL from `.sanho.json`. Because the clone is private to the workspace, there is no shared clone directory and no name-collision surface — audit M9 (basename-keyed shared clones silently colliding) is resolved by construction. The registry (§5.7) stores full URLs.
- **Branch**: canonical publication targets `main`, falling back to `master` if `main` does not exist on origin (carried over from v0.1). The resolved name is cached in the clone config at init.
- **Fetch policy**: explicit fetch at the start of `sync`, `pull`, `publish` (pre-push), and `status --refresh`. No background fetching, no polling. The last successful fetch time is recorded (`.git/sanho/canonical/sanho-last-fetch` timestamp file) and displayed as data age wherever cached results are served.
- **Offline behavior**: read paths (`status`, `state`, pre-commit detection) serve the last-fetched state with an explicit staleness line (`canonical data as of 2026-08-07 09:14 (3h ago) — run 'sanho status --refresh'`). Write paths (`sync`, publication) require a successful fetch and fail closed with an actionable message (§5.9); this asymmetry is deliberate — pushing already implies network access.
- **Object exchange with the app repo** uses local git transport, both directions:
  - app → clone: `git -C <clone> fetch <app-git-dir> <tip>` brings the app tip (including its docs subtree) into the clone's object database. Docs trees are then addressed directly by OID (`<fetched-tip>:<docsDir>`).
  - clone → app: `git -C <app> fetch <clone> <canonical-ref>` brings canonical commits into the app's object database for local merging and checkout.

  All content therefore moves as git objects — blobs, trees, **symlinks, and file modes are handled by git natively**. The v0.1 tar-snapshot subsystem is not used on any v0.2 path (this retires audit H1 wholesale rather than patching it).

### 5.3 Publication algorithm (pre-push)

Inputs: the ref updates on the hook's stdin; the base file `{commit B, tree}`; the private clone.

1. Filter updates: consider only `refs/heads/*` updates with a non-zero local OID. Tag pushes and branch deletions pass through untouched. If nothing remains, exit 0.
2. If `.git/sanho/sync.json` exists (a conflicted sync is in progress), reject: `finish the sync first: resolve conflicts, then 'git add' and 'git commit' (or 'sanho sync --abort')`.
3. Marker gate: scan the docs blobs of each pushed tip (detector per §5.4). Any hit rejects the push naming the files. This prevents committed-but-unresolved markers from ever reaching canonical.
4. `git -C <clone> fetch origin` (bounded timeout; on failure reject with the canonical-unreachable message, §5.9). Let `H` = canonical head, `Ht` = its docs tree.
5. For each pushed tip, compute `T` = tip's docs tree (empty tree if the docs dir is absent). Deduplicate identical `T`s. Then per unique `T`, in stdin order:
   - **Case ①** `T == Ht`: nothing to publish. Continue.
   - **Case ②** `B == H` and `T != Ht`: fast-forward publish. In the clone: fetch the tip from the app repo; `git commit-tree` a new commit whose tree is the canonical root tree with the docs subtree replaced by `T` (for a docs-only canonical repo, the root tree *is* the docs tree), parent `H`, author/committer from §"canonical commit convention" below; `git push origin <new>:refs/heads/<main>`. On non-FF rejection (a racing publisher won): refetch and re-enter case analysis, at most 3 attempts, then reject the push with the sync guidance.
   - **Case ③** `B` is an ancestor of `H`, `T != Ht`: three-way merge `merge(base = B's docs tree, ours = T, theirs = Ht)` computed in the clone via §5.4. If **clean**: publish the merged tree as in case ② (parent `H`) and continue the push in the same invocation — zero user friction. If **conflicted**: reject the push listing conflicted files and instructing `sanho sync`.
   - **Case ④** `B` unknown to canonical (history rewritten): attempt re-anchoring — search canonical history for a commit whose docs tree equals `docs-base-tree` (§5.1); if found, adopt it as `B` and re-enter. If not found, reject with the rewrite-recovery message (§5.9); `sanho sync --rebase-onto <commit>` is the manual escape (§5.5).
6. After any successful publish: print the published canonical OID; then apply the base-advance rule — **if the docs worktree tree equals the newly published tree, set the base file to the new canonical commit; otherwise leave the base file unchanged** (see invariant, §5.7).

**Worktree inviolability.** Pre-push never modifies the working tree, the index, or any app ref — matching git's own push semantics. After a case-③ clean publish the worktree is intentionally left as-is; `status` will show "behind (your own merge)" and `sanho pull` fast-forwards it.

**Canonical commit convention.** One commit per publish:

```
author/committer:  <actor_email from .sanho.json>
subject:           docs: <workspace-repo-name>/<branch> (<N> app commits)
body:              source: <workspace-id> @ <app tip OID>
                   commits:
                     - <subject of each app commit since B that touched docs>
```

Granularity is per-push; traceability of the constituent app commits is preserved in the body. Canonical history stays linear (no merge commits).

### 5.4 Merge primitives

**Tree-level 3-way merge** uses `git merge-tree --write-tree` (git ≥ 2.38), executed in whichever repo holds all three trees (the clone for publication, the app repo for sync):

```
git merge-tree --write-tree --merge-base=<baseCommit> <oursCommit> <theirsCommit>
```

Inputs are wrapped as parentless synthetic commits over the docs trees (`git commit-tree <tree>` with fixed metadata) so that only docs content participates. The command returns the result tree OID, a clean/conflict status, and per-file conflict info; conflicted blobs contain standard markers. Marker labels: create temporary refs named `sanho-ours` and `sanho-upstream` for the two sides so markers read `<<<<<<< sanho-ours` / `>>>>>>> sanho-upstream` instead of raw OIDs or temp paths (fixes audit L1).

**Git version policy (decided 2026-08-07): not enforced.** Sanho uses whatever git is installed; `init` does not gate on a version and `doctor` merely reports the detected version. `merge-tree --write-tree` requires git ≥ 2.38 in practice — on an older git the command fails with a clear git error, which is the intended surface: handle via bug report if real demand appears, rather than pre-building a fallback. If a `git merge-file` fallback is ever added, it **must** implement the corrected exit-code contract — exit 0 = clean, **1–127 = number of conflicts (a conflicted merge, not an error)**, ≥ 128 or signal death = hard error. The v0.1 misreading of this contract was Critical C2; any fallback shipping without this contract and a real-git test matrix (0/1/2/3-hunk conflicts) repeats it.

**Marker detector** (used by the pre-push gate and sync/commit gates), replacing the v0.1 detector with its three silent failure modes (audit H2):

- Operates on git blobs (tip scan) and worktree files (sync-in-progress scan).
- Binary skip: a NUL byte in the first 8 KiB classifies the file as binary → skipped (v0.1 false-positived on binaries).
- No line-length blindness: detection is substring-based over the full content with a bounded read (files > 10 MiB are reported as "too large to scan" **errors**, not silently passed). v0.1's 64 KiB `bufio.Scanner` limit silently passed long-lined files.
- **Errors propagate.** Any unreadable file fails the gate closed with the file named. v0.1 swallowed errors with an unimplemented "log warning" comment.

### 5.5 `sanho sync` and `sanho pull`

Two verbs, two intents:

- **`sanho pull`** — *consume only*. Requires that local docs have no edits relative to the base (worktree docs tree == base tree, nothing docs-related staged). Fast-forwards `docs/` to canonical HEAD, updates the base file. Refuses otherwise, pointing at `sync`. `--commit` additionally records the update as a `docs: sync to <oid>` commit (identical convention to sync) for users who want the base bump in history immediately.
- **`sanho sync`** — *reconcile*. The workhorse. Flow:

1. Require a managed workspace; require no sync already in progress; require the **docs paths** clean in worktree and index relative to `HEAD` (other paths may be dirty — the user's non-docs work in progress is none of sync's business). If docs are dirty: refuse with `commit or stash your docs changes first` (fail-closed; sync runs at user pace, so asking is acceptable — this requirement is what lets v0.2 delete the 4-layer preservation machinery).
2. Fetch canonical; fetch canonical head `H` into the app repo's object database.
3. If base `B == H` and tip docs tree == `Ht`: report up-to-date, exit 0.
4. Run the §5.4 merge (base = `B`'s docs tree, ours = `HEAD`'s docs tree, theirs = `Ht`) in the app repo.
5. **Clean**: check out the result tree into `docs/` (index updated for docs paths only), write the base file `{H, Ht}`, and create the commit `docs: sync to <short H>` restricted to the docs pathspec (`git commit -m … -- <docsDir>` semantics — the user's staged non-docs work is untouched). Done — subsequent user commits sit on base `H`, and their diffs contain only user changes.
6. **Conflicted**: check out the result tree (with markers) into `docs/`; write `.git/sanho/sync.json` `{prev_base: {B, Bt}, target: {H, Ht}, started_at}`; set the base file to `{H, Ht}` (the resolution, once made, derives from `H` — this makes the eventual resolution commit stamp `docs-base: H`, which is correct); print the §5.9 conflict message. The user resolves, `git add`, `git commit` — an ordinary commit; `post-commit` is not needed because nothing else must happen: the next push publishes the resolved tree via case ②.
7. **`sanho sync --abort`** (valid whenever `sync.json` exists): `git checkout HEAD -- <docsDir>` (restores pre-sync docs), restore the base file from `prev_base`, delete `sync.json`. This sequence touches only the docs worktree/index and two state files; there is no state in which it can fail — guidance closure by construction.
8. **`sanho sync --rebase-onto <canonical-commit>`** (rewrite recovery, case ④): identical flow with the explicit commit as `theirs` target and base re-anchored by `docs-base-tree` search; documented in the recovery guide.

### 5.6 Detection (pre-commit)

Strictly local; the commit path never opens a network connection and never blocks on canonical availability (P2).

1. Marker gate on **staged** docs content (§5.4 detector): committing unresolved conflict markers is blocked (this is also what turns a resolved sync into a committable state — resolved files pass).
2. Freshness: compare the base file against the private clone's last-fetched canonical head. Output contract:
   - up to date → **silent** (v0.1 printed a sync line on every commit; v0.2 treats silence as the success signal),
   - behind and a merge would be clean (cheap local `merge-tree` pre-check) → one line: `sanho: docs base is N commits behind — 'sanho sync' will merge cleanly`,
   - behind with conflicts predicted → `sanho: docs base is N commits behind — 'sanho sync' will report conflicts in <files>; syncing sooner keeps them small`,
   - fetch data older than 24 h → append `(canonical last checked <age> ago)`.
3. Exit code is always 0 for freshness states; only the marker gate (and a corrupt-workspace config) can fail the hook.

### 5.7 State, files, and locking

**Base file — `.sanho_base.json`** (workspace root, gitignored; replaces `.sanho_docs_hash`):

```json
{ "version": 2,
  "commit": "<canonical commit OID>",
  "tree":   "<its docs tree OID>" }
```

*Invariant*: the base file always answers "which canonical state do the **worktree docs** derive from." Consequently only operations that change the docs worktree move it (`pull`, `sync`, checkout re-derivation) — with the single §5.3 exception (post-publish advance when trees are identical, which preserves the invariant trivially). Reads tolerate the v0.1 single-line format (§8). All writes go through the shared atomic writer (below).

**Workspace config — `.sanho.json`** gains `"schema_version": 2` and `"docs_repo_url"` (the CLI must know the URL directly — no daemon to resolve it), and drops `socket_path`.

**Registry — `~/.sanho/state.json`** (plus `state.json.bak`):

```json
{ "version": 2,
  "projects":   { "<name>": { "docs_repo_url": "<url>" } },
  "workspaces": { "<project>:<abs-path>": {
       "project": "…", "local_path": "…",
       "base_commit": "…", "base_tree": "…",
       "last_updated_at": "RFC3339", "actor_email": "…" } } }
```

Access protocol: open/create `~/.sanho/state.lock`, take an **exclusive `flock`** (5 s timeout → fail with a clear message naming the lock path), read-modify-write `state.json` via the atomic writer, write `.bak`, release. Every CLI invocation that changes workspace state (init, sync, pull, publish, clean) updates its own entry inside the lock; `status`/`state` read under the same lock. Corruption recovery: primary unreadable → restore from `.bak`; both unreadable → refuse with instructions (never silently start empty) — semantics carried over from the v0.1 daemon.

**Atomic writer (shared utility).** The v0.1 daemon's `writeAtomic` (unique temp name → chmod → write → fsync file → rename → fsync directory) is promoted to a shared package and used for **every** state write: registry, base file, workspace config, sync note, hook files. This closes audit M5 (the client side previously used fixed-name temp files without fsync, and raw `os.WriteFile` for the hash file).

**Sync note — `.git/sanho/sync.json`**: exists only between a conflicted `sync` and its resolution/abort; schema in §5.5.

**Deleted state** (nothing replaces them): pull-commit transaction directory and artifacts, pulled-docs baseline, main-publication store, workspace-report retry file, `.sanho_pending_fix`.

### 5.8 Command surface

| Command | v0.2 behavior | Change vs v0.1 |
|---|---|---|
| `sanho init` | Registers project+workspace in the registry, writes v2 config/base file, clones canonical into `.git/sanho/canonical`, installs 6 hooks, `.gitignore` entries. Reuse mode (existing docs + stamped commits) supported as today. | No daemon prompt/socket; `--docs-repo-url` recorded locally |
| `sanho status` | Base vs canonical (cached fetch + age), sync preview (clean/conflict), sibling table from registry with relations computed in the local clone, marker/sync-in-progress state. `--refresh` fetches first. `--json` stable schema. | Daemon fields, 5 pending states, publication/transaction sections all gone; add `data_age`, `sync_preview` |
| `sanho state [--all]` | Registry dump + per-project canonical heads (cached). | Reads file, not socket |
| `sanho sync` | §5.5. `--abort`, `--rebase-onto <oid>`, `--json`. | New — replaces `pull-commit`/`fix` |
| `sanho pull` | Fast-forward consume; `--commit`. | Refuses on local docs edits (points to sync) instead of baseline magic |
| `sanho clean` | Removes registry entry, hooks (exact-line, §5.10), config/base files, private clone; `--remove-docs` optional. **`--dry-run` is strictly read-only** (audit M4 was a dry-run that deleted state — regression-tested). | No daemon call; far fewer blockers (no transactions to protect) |
| `sanho doctor` | Checks: git version (informational — no minimum enforced), hooks present/exact/not duplicated, clone health + origin reachability, base file validity (and `--fix` re-derivation per §5.10), registry lock health, sync-in-progress status, docs symlink/size inventory. `--json`. | New |
| `sanho project add/delete` | Registry admin (file-based). | Same UX, no daemon |
| `sanho hook <name>` | The 6 hook entry points. | `post-commit`, legacy `fix`, `pull-commit` removed |
| `sanho version` | Unchanged. | — |

Exit codes: 0 success · 1 user-actionable · 2 internal bug (unchanged). Stable machine error codes (superset carried over): `not_in_workspace`, `invalid_workspace_config`, `base_file_invalid`, `canonical_unreachable`, `sync_required`, `sync_in_progress`, `sync_conflicts`, `markers_present`, `registry_lock_timeout`, `git_too_old`, `history_rewritten`. `--json` is available on **all** read commands and on `sync` (the audit flagged JSON-less mutating commands as an agent-workflow gap).

### 5.9 Guidance contract

Normative principle (from D3): *every advised command must succeed in the advising state; enforced by table-driven tests* (§9). The three human touchpoints and their canonical templates:

1. **Commit warning** (informational, never blocks — §5.6):
```
sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly
```
The message must always say what happens if ignored when relevant (e.g. conflict prediction: "syncing sooner keeps them small").

2. **Sync conflict**:
```
sanho: merged docs with upstream — 2 files have conflicts:
  docs/api.md
  docs/schema.md
Resolve the markers, then:  git add docs/ && git commit
To undo this sync:          sanho sync --abort
```

3. **Push rejection** (case ③-conflict / ④):
```
sanho: your docs changes conflict with upstream (base 67c4bbf → 9a41f2c)
Run 'sanho sync', resolve, commit, then push again.
error: push rejected — no remote ref was changed
```

Message hygiene rules: English only (audit L4; enforced by a lint grep in `make test`); name the exact next command; never print raw Go error chains at user level (wrap with cause + action); include OIDs shortened to 12 chars; degraded-mode lines always include data age.

### 5.10 Hooks inventory

| Hook | v0.2 role | Notes |
|---|---|---|
| `pre-commit` | Staged marker gate + local freshness warning (§5.6) | No network, no daemon |
| `commit-msg` | Stamp `docs-base` / `docs-base-tree` (§5.1) | Never blocks |
| `pre-push` | Publication (§5.3) + marker gate + sync-in-progress gate | The only network hook |
| `post-checkout` | Re-derive the base file for the new HEAD | See below |
| `post-merge` | Same re-derivation | |
| `post-rewrite` | Same re-derivation (amend/rebase moved HEAD) | |
| ~~`post-commit`~~ | **Removed** — commits don't move the base (invariant §5.7), and registry updates ride on sync/pull/publish | |

**Base re-derivation** (post-checkout/merge/rewrite): the base is a property of the checked-out content, so after HEAD moves, recompute it locally: walk `git log HEAD --grep=docs-base --grep=docs-version` for the newest reachable stamped commit; adopt its base pair (for legacy `docs-version: X`, adopt `{X, tree-of-X-if-cached-else-unset}`); if the worktree docs differ from HEAD's docs (uncommitted edits carried across checkout), keep the current base file untouched and let `doctor` flag any inconsistency. Always exit 0.

**Installation** (init) / removal (clean): **exact-line matching** for all six hooks (v0.1's substring `strings.Contains` check was the root of the double-install class, audit L3/A3.6; the pre-push-only migration special case generalizes to all hooks). Writes use the atomic writer; a hook file left with only its shebang after removal is deleted (audit L5).

---

## 6. What is removed

Deleted outright, with the decision that retires each:

| Removed | Retired by | Was |
|---|---|---|
| `sanhod` binary, Unix socket, entire HTTP surface (server, handlers, DTOs, httpclient) | D4 | ~daemon + transport layer |
| Pull-commit engine, 5-phase transaction, recovery classifier, recovery refs | D3 | `pull_commit_engine.go` (1,076 lines) + `pull_commit_recovery.go` |
| Dirty-layer capture / throwaway-clone rebase / atomic `update-ref` of main+branch | D3/P3 | `main_sync.go` (~500 lines); also removes the silent local-`main` advance (M2) |
| Main-publication contract and store | D1 (no app-side sync commits ⇒ no ordering constraint) | `main_publication*.go` |
| Pulled-docs baseline (deferred materialization) | D3 (`pull --commit` is explicit) | `pulled_docs*.go` |
| Legacy pending-fix flow and `sanho fix` | already dead in v0.1 production | `fix.go`, `handleOutdated` |
| `[SANHO] Update docs` commits | P3 | tool-authored commits |
| Reported-hash validation guard | D4 (registry is observational) | `report_docs_hash.go` guard (C3's trigger) |
| tar snapshot transport on merge/publish/consume paths | §5.2 git-native object exchange | `snapshot_builder/applier` on these paths |
| `pull-commit`, `fix`, `workspace register/unregister` (folded into init/clean), `post-commit` hook | various | CLI surface |

The five v0.1 pending states (`pending_fix`, `pull_commit`, `main_publish`, `head_reconcile`, plus the pulled-docs baseline) collapse into **one**: "a conflicted sync is in progress" (`sync.json` exists).

---

## 7. Defect disposition — the 26 audit findings

"Resolved by design" = the defective component or precondition no longer exists in v0.2 (no fix to write, but §9 adds a regression test where meaningful). "Must fix in v0.2" = carried obligations that v0.2 code must implement correctly.

| ID | Finding (v0.1) | Disposition in v0.2 |
|---|---|---|
| C1 | Daemon down ⇒ all commits blocked | **Resolved by design** — commit path is local-only (P2, P4) |
| C2 | `merge-file` exit-code misread ⇒ 2-hunk wedge | **Resolved by design** — `merge-tree` primary (§5.4). *Must-fix rider*: any `merge-file` fallback ships with the corrected 1–127 contract + real-git tests |
| C3 | SyncCommitted permanent wedge; guidance points at failing command | **Resolved by design** — no transactions; `sync --abort` cannot fail; guidance closure tested |
| H1 | Symlink kills snapshot build / silent drop on apply | **Resolved by design** — content moves as git objects (§5.2); add symlink round-trip e2e |
| H2 | Marker detector: swallowed errors, 64 KiB blindness, binary false positives | **Must fix** — new detector spec §5.4 |
| H3 | Daemon startup hang, unbounded handler block, no timeouts | **Resolved by design** — no daemon |
| H4 | Reword-amend silently strips trailer; repair deadlocked | **Resolved by design** — stamp rule covers amends; restamp is offline; trailers non-gating (§5.1) |
| H5 | Circular recovery guidance | **Resolved by principle** — guidance closure is normative + tested (D3, §9) |
| M1 | Publish-on-commit bypasses review; aborted commits publish | **Largely resolved** — publish at push; aborted commits never publish. Residual: branch pushes publish pre-merge; policy knob reserved (§10) |
| M2 | Feature commit silently advances local `main` | **Resolved by design** — no ref manipulation (P3) |
| M3 | No cross-process locking anywhere | **Resolved by design** — publish CAS is git-remote-side; registry lock is a *specified deliverable* (flock, §5.7) with concurrency tests |
| M4 | `clean --dry-run` mutates state | **Must fix** — dry-run strictly read-only; regression test |
| M5 | Client-side non-atomic writes (fixed tmp names, no fsync, raw WriteFile) | **Must fix** — shared atomic writer everywhere (§5.7) |
| M6 | First commit always fails on stale base; breaks tooling | **Resolved by design** — commits never fail for freshness (P2) |
| M7 | Undocumented HTTP endpoint vs docs-authority principle | **Moot** — HTTP surface removed. Follow-through: keep `docs/architecture.md` in lock-step at v0.2 release |
| M8 | Unbounded base64-in-JSON snapshot bodies | **Moot** — no HTTP transport |
| M9 | `docs_repo_id` basename collisions overwrite registrations | **Resolved by design** — per-workspace clones; registry stores full URLs (§5.2, §5.7) |
| M10 | >104-char socket path ⇒ raw bind error | **Moot** — no socket |
| L1 | Conflict markers labeled with temp paths | **Must fix** — named refs `sanho-ours`/`sanho-upstream` (§5.4) |
| L2 | Dead legacy pending-fix code kept as blockers | **Resolved** — deleted (§6) |
| L3 | Substring hook-idempotency; migration special-cased to one hook | **Must fix** — exact-line matching for all hooks (§5.10) |
| L4 | Korean string in English CLI (init reuse path) | **Must fix** — English-only + lint (§5.9) |
| L5 | `clean` leaves shebang-only hook files | **Must fix** — §5.10 |
| L6 | 7× duplicated legacy-removal call in clean loop | **Resolved** — rewrite |
| L7 | Three divergent git-exec layers; no env/timeout policy anywhere | **Must fix** — single git runner: argv-only (no shell), `GIT_TERMINAL_PROMPT=0`, `GIT_SSH_COMMAND="ssh -o BatchMode=yes"` on network ops, per-command timeouts, uniform exit-code classification |
| L8 | Unstructured logging; ignored error returns | **Must fix** — `log/slog`; `errcheck` in lint |

Carried-over v0.1 strengths that are **regression requirements**, not options: zero-data-loss behavior in every flow, path-traversal defenses wherever trees are checked out, argv-only git invocation (no shell), 0600/0700 permissions on `~/.sanho`, cobra-only runtime dependency footprint, and the machine-readable error-code discipline.

---

## 8. Migration from v0.1.x

Design constraints: the maintainer uses Sanho in production; migration must be a short, reversible, per-machine procedure with no history rewriting.

**Compatibility facts** (by construction):
- Old `docs-version:` trailers and `[SANHO] Update docs` commits are inert history; the re-derivation scan understands the old key (§5.1), so v0.2 works immediately on top of v0.1 history.
- The canonical repository itself is untouched — same repo, same linear main, new commit-message convention going forward.

**Procedure — `sanho migrate` (new command, idempotent):**
1. Preconditions: v0.2 binary installed; refuse if a v0.1 pull-commit transaction or pending-fix state exists (finish or abort it on v0.1 first — the one thing v0.2 will not interpret).
2. Stop and unload the daemon if running (print the exact `launchctl bootout` / `systemctl --user disable --now` line; do not do it for the user — service ownership was explicitly the user's in v0.1).
3. Read legacy `~/.sanho/state.json` (daemon schema) → build the v2 registry: project→URL map carries over; workspace entries carry over with `base_commit` from each workspace's `.sanho_docs_hash`.
4. Per workspace: write v2 `.sanho.json` (add `docs_repo_url`, drop `socket_path`); create `.git/sanho/canonical` clone; fetch; resolve `base_tree` for the recorded base commit (if the commit no longer exists → leave tree unset and print the rewrite-recovery note); write `.sanho_base.json`; **rewrite hooks**: remove all seven v0.1 lines (exact-line), install the six v0.2 lines; update `.gitignore` (`.sanho_base.json` added; old names kept ignored).
5. Remove daemon-era files only on `--purge` (state backups kept otherwise). Binaries: the user deletes `sanhod` at leisure; nothing references it.
6. Every rewritten file gets a `.bak` sibling. **Rollback** = reinstall v0.1 binaries, restore `.bak` files, restart the daemon.

**Pre-migration degradation (normative).** Because hook lines invoke `sanho` by name, installing the v0.2 binary routes v0.1-era hooks to the new binary before `migrate` has run. On detecting a v1 workspace (`schema_version` absent / `socket_path` present), v0.2 hook entry points degrade safely rather than half-operating: `pre-commit`, `commit-msg`, and the `post-*` hooks print nothing (or a single migrate hint) and exit 0 — commits keep working throughout the transition — while `pre-push` fails closed with `run 'sanho migrate' first`. The push boundary is thus the natural migration prompt, and no commit is ever blocked by the upgrade.

**Mixed-version operation** on a single machine/workspace (v0.2 hooks with v0.1 state) is not supported and is mechanically prevented as above; `migrate` additionally refuses on live v0.1 transactions (step 1). Different machines may temporarily run different versions against the same canonical repository — canonical is a plain git repo and both versions' publications serialize through git itself — but this is a transition state, not a supported configuration. Migrate one machine at a time.

---

## 9. Testing strategy

The audit's sharpest lesson: **every confirmed implementation bug hid behind a mock** (`merge_file.go` had zero tests; `MergeDirectories` was tested only against a fake merger; symlinks appeared in zero builder/applier tests). v0.2 testing rules:

1. **No mocks below the git boundary.** Merge, publication, re-derivation, and detector logic are tested against real `git` in temp repos. The merge test matrix includes 0/1/2/3-hunk conflicts (C2's exact blind spot), symlinks, mode changes, binary files, deletes on both sides, and empty/absent docs dirs.
2. **Guidance-closure suite.** A table enumerating every (state, emitted message) pair; the test machinery parses the advised command out of the message, runs it in that state, and asserts success. Adding a message without a table entry fails the build.
3. **Concurrency.** Two workspaces publishing to one canonical concurrently (CAS retry path); N processes hammering the registry flock; `make test` runs with `-race`.
4. **Migration fixtures.** A scripted v0.1 workspace (with `[SANHO]` commits, `docs-version` trailers, live hash file) → `sanho migrate` → full v0.2 flow e2e; plus the refusal paths (live transaction).
5. **Scenario e2e (the audit's S-matrix, rewritten for v0.2 semantics):** onboarding fresh + reuse; propagation and pull; same-file 1-hunk and **2-hunk** conflicts via sync (both must reach resolution); different-file concurrency; offline commit (must succeed) and offline push (must fail closed with correct message); amend/reword then push (must publish — H4's trap); branch switch re-derivation; clean and re-init; `clean --dry-run` byte-for-byte no-op check.
6. **Retired suites** (delete, don't port): daemon HTTP integration/e2e, pull-commit engine phases, main-publication ordering, legacy-hook upgrade, pending-fix flows.
7. Keep: architecture layering test (extend rules: state machines live outside `interface/`), package-ownership gate, docs-freshness gate (update file list), install test. Add CI (`make test` on push) — the audit found no CI at all; the manual H-scenario checklist becomes a release complement, not the only gate.

---

## 10. Future work (explicitly out of scope for v0.2)

- **Team visibility across machines**: carry workspace state as lightweight refs (`refs/sanho/workspaces/<id>`) in the canonical repo. Exceeds what the daemon ever offered; adds ref-hygiene concerns — design separately.
- **Server process revival**: a watcher / shared status server can be layered on top of `~/.sanho/state.json` without changing this architecture (D4).
- **Publish policy for teams**: `publish_branches: any | main-only` in `.sanho.json`, closing M1's residual (docs publish before PR review) for review-centric teams. The §5.3 algorithm has a single publish decision point to host this.
- **`on_stale_base: auto-sync`** opt-in (hook-time auto-sync accepting the retry dance) — only if real usage demands it.
- **Queue-and-replay** per-commit canonical granularity — only if a concrete need for intermediate states in canonical appears.

## 11. Open questions (safe to decide during implementation)

1. ~~git < 2.38 fallback~~ — **decided 2026-08-07**: no version enforcement, no pre-built fallback; use the installed git and respond to real bug reports (§5.4).
2. **`pull` retention**: keep `pull` as a separate verb or make it `sync --ff-only`? (Spec keeps both; collapsing is a UX-only change.)
3. **Freshness pre-check cost**: the clean/conflict prediction in the commit warning runs a local `merge-tree`; if measured cost on large docs trees is noticeable, degrade the message to behind-count only.
4. **Registry pruning**: policy for workspaces whose paths no longer exist (doctor warns vs auto-prune).

---

*Prepared from the 2026-08-07 audit (26 findings, sandbox scenarios S0–S9) and the subsequent design review that produced decisions D1–D4. The audit report and evidence transcripts are session artifacts; the defect table in §7 is self-contained.*
