# Sanho Hands-on Testing

This checklist covers boundaries that the automatic suite cannot reproduce
faithfully. Run it only with a release candidate that already passed
`gaori --json run all` or the equivalent `make test`.

Automatic tests remain authoritative for deterministic behavior: publication
case analysis, real-Git merge flows, marker gates, sync continuation and abort,
registry safety, hook ownership, linked-worktree isolation, process concurrency,
guidance closure, and installability. Hands-on testing adds real hosting,
credentials, network loss, filesystem behavior, and installed-binary context.

## Rules

- Record every repository and the authorized write scope before starting.
- Use a fresh checkout-built candidate and an isolated `SANHO_HOME` for fixtures.
- Prefer disposable application repositories and branches.
- Never use `--no-verify`, force operations, or manual deletion of managed state
  to turn a failed case into a pass.
- Record ref OIDs before and after negative tests. A rejected write must leave
  the relevant remote unchanged.
- Do not push, tag, release, or install Sanho itself without separate approval.

## Run record

Capture at least:

```text
Run ID and time:
Operator, OS, Git, Go:
Candidate commit and binary SHA-256:
sanho version --json:
Absolute sanho path:
SANHO_HOME:
Repositories and authorized writes:
Starting app/canonical refs:
Starting git status and sanho status --json:
Commands and exit codes:
Relevant stdout/stderr:
Ending refs, base, status, and doctor:
Cleanup and retained evidence:
Verdict: PASS / FAIL / BLOCKED
```

Common preflight:

```bash
command -v sanho
sanho version --json
git --version
git status --porcelain=v1
git remote -v
sanho status --json
sanho doctor --json
```

Read canonical head from hosting or the private clone:

```bash
git ls-remote --heads <docs-repo-url> refs/heads/main
CLONE="$(git rev-parse --path-format=absolute --git-common-dir)/sanho/canonical"
git -C "$CLONE" log --oneline -5 refs/remotes/origin/main
```

## Boundaries kept automatic

| Boundary | Automatic evidence |
|---|---|
| Cross-machine publication races | E2E processes with separate `SANHO_HOME` values prove linear CAS publication and explicit conflict recovery |
| Server-side rejection and publication branch choice | Integration uses real bare remotes and rejection hooks; canonical tests cover main/master selection |
| Large docs correctness and measurement | `SANHO_SCALE=1 make test-scale` exercises 1,000 files, 500 commits, and about 50 MiB |

## H01. Real three-repository roundtrip

Use real disposable or explicitly approved `sanho-server`, `sanho-client`, and
`sanho-docs` repositories.

1. Record all three starting refs and clean statuses.
2. Initialize both application clones with the candidate and isolated home.
3. Change docs in server, commit, and push. Confirm a canonical commit appears
   with `[SANHO] Publish docs from <repo>/<branch> (<N> app commits)` plus
   `source` and `commits`.
4. Refresh client status, sync, and confirm its docs tree equals canonical.
   Before syncing, confirm `sanho diff --refresh` shows the incoming change and
   does not alter the application worktree, index, refs, or base.
5. Change docs in client, publish, then sync server.
6. Push a code-only commit and confirm canonical does not move.
7. Confirm tag-only and branch-deletion pushes do not run publication analysis.
8. Record final app/canonical refs, bases, status, and doctor output.

Sanho moving or pushing an application ref is a failure.

## H02. Offline commit and fail-closed push

1. Populate the cache with `sanho status --refresh`.
2. Disconnect network using an approved OS-level method or a disposable
   transport shim that fails the SSH executable Sanho actually invokes.
3. Confirm code and docs commits, amend, branch creation, rebase, cached status,
   state, and doctor remain available.
4. Confirm a docs push fails with canonical-unreachable guidance and that both
   application and canonical remote refs remain byte-identical.
5. Confirm explicit network reads such as `status --refresh` fail clearly.
6. Restore the network and retry the same push without deleting state. Confirm
   every offline docs commit is represented by the resulting publication.

An offline commit failure is a test failure. Inherited `GIT_SSH_COMMAND` is not
a valid injection because Sanho intentionally replaces it with its own
non-interactive SSH command.

## H03. Linked worktrees and shared ownership

1. Create two linked worktrees of one disposable application repository.
2. Initialize once and confirm both resolve the same private clone under the Git
   common directory and the same shared default hook directory.
3. Create a conflicted sync in one worktree. Its `sync.json` must exist only in
   that worktree's private Git directory; the sibling remains operable.
4. Confirm push is blocked in the conflicted worktree and remains available in
   the sibling when its own docs state permits it.
5. Run near-simultaneous pushes and confirm canonical remains linear.
6. Clean one worktree. The dry run must be read-only, name the sibling, and
   preserve shared hooks/clone. Clean the last owner and confirm shared assets
   are then removed.
7. Delete a disposable independent checkout without cleaning it, then use its
   exact registry ID with `sanho workspace forget`. Confirm only that stale row
   disappears and a live-row forget attempt is refused.

## H04. Custom hooks and Husky ownership

Prepare disposable repositories using a repository-local `.githooks` path and a
real Husky 9 `.husky/_` layout. Record script bytes, modes, tracked state, and
Husky shim hashes.

1. Run `sanho init` without opt-in in both repositories. It must name and reject
   the custom path before config, registry, clone, docs, or hooks change.
2. Initialize with `--manage-custom-hooks`. Confirm `.sanho.json` records
   `hook_mode` and a worktree-relative `hook_dir`.
3. Confirm six portable Sanho calls appear exactly once in user scripts, while
   foreign commands, modes, and every generated Husky shim remain unchanged.
4. Exercise a real commit and push through both layouts.
5. Remove a managed line and add an obsolete/duplicate recognized line. Doctor
   must report it and `doctor --fix` must restore exactly one current line
   without hiding a foreign command's failure status.
6. Change `core.hooksPath` after initialization. Doctor must warn and modify
   neither the old nor new directory.
7. Confirm global, repository-external, unrecognized Husky, and symlink paths
   remain fail-closed even with opt-in.
8. Run clean dry-run and clean. Only Sanho lines may disappear; foreign scripts,
   comments, shebangs, modes, and generated shims must remain.

## H05. SSH and network retry

1. Use a candidate hook and a real or approved disposable SSH remote.
2. Confirm every network call is non-interactive and bounded.
3. Inject DNS, route, authentication, and early transport failures where the
   environment permits it.
4. For each failure, record exit code and both remote refs. No rejected attempt
   may move either ref or leave a transaction to clean manually.
5. Restore transport and retry the identical command. It must succeed without
   deleting the clone, base, registry, or hook state.

## H06. Canonical history rewrite recovery

Use disposable local bare repositories. Introduce rewrites by preparing an
independent replacement repository and atomically swapping the remote path; do
not force-push a shared remote.

1. Replace canonical history with an unrelated commit carrying the identical
   tree. Refresh and confirm automatic tree-based re-derivation.
2. Replace history and content. Publication must fail without changing either
   remote and print a usable `--rebase-onto` candidate.
3. Reject a nonexistent target.
4. Resolve against the candidate while intentionally omitting a canonical-only
   file. Completion/publication must refuse the failed absorption proof.
5. Restore the file, amend the resolution, continue, and publish successfully.
6. Repeat branch guidance against a master-only canonical and confirm Sanho uses
   `refs/remotes/origin/master`, never `origin/HEAD`.
7. Confirm final canonical history is linear and contains all intended files.

## H07. Symlink, mode, and binary roundtrip

1. In a disposable application repo, add regular text, executable files,
   symlinks to files and directories, binary data with NUL bytes, long lines,
   spaces, Unicode names, empty directories where representable, additions,
   edits, and deletions.
2. Publish to canonical and sync a second application repository.
3. Compare Git tree OIDs and `git ls-tree` modes across source, canonical, and
   destination. Compare file hashes and symlink targets.
4. Confirm marker scanning skips binary files, preserves symlinks, reports an
   oversized text file safely, and detects only complete marker sequences.

The three docs tree OIDs must be identical after convergence.

## H08. Installed binary and PATH boundary

1. Build from a clean detached source into an isolated `GOBIN`. Record candidate
   commit, binary SHA-256, `sanho version --json`, and `go version -m`.
2. Confirm build metadata names the candidate revision with `vcs.modified=false`.
3. Confirm default hooks contain the shell-quoted canonical absolute binary
   path. Remove that binary temporarily: commit/post hooks must stand down and
   pre-push must fail closed. Restore it immediately.
4. Confirm opt-in custom/Husky hooks use portable `sanho`. With a restricted PATH
   that lacks Sanho, commit/post hooks must pass and pre-push must fail closed.
5. Invoke Git from a GUI or another environment with a materially different PATH
   when available, and verify the same boundary.
6. Use the isolated candidate in a disposable workspace for commit, sync, push,
   status text/JSON, and doctor. No production binary may satisfy the test by
   accident.

## Release verdict

A candidate is releasable only when:

- the required automatic suite passes;
- H01 through H08 are PASS, or a skipped item has an explicit scope-based
  justification accepted by the release owner;
- negative tests prove ref invariants rather than merely showing an error;
- retained evidence identifies candidate commit and binary;
- the release diff and results are presented for separate final approval.

Hands-on evidence never grants permission to commit, push, tag, publish a
release, or install a production binary.
