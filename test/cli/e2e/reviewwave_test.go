package e2e

// The external-review fix wave, reproduced end to end.
//
// Each test below is one reviewer scenario, written the way the reviewer
// described it and asserted on what the repositories afterwards contain.
// Every one of these failures was invisible from inside the code — exit
// 0, a plausible message, a silent publication — which is why they are
// black-box tests against the real binary and real git.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// markerDoc is a docs file carrying an unresolved conflict, in the §5.4
// detector's own marker vocabulary.
const markerDoc = "<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n"

// --- X1: a stash is not a resolution ------------------------------------

// TestStashingAConflictedSyncDoesNotCountAsResolving is the Critical.
//
// `sanho sync` conflicts; the user runs `git stash push -- docs` instead
// of resolving. The docs are then clean and no file carries a marker, so
// the old completion test declared the sync resolved and cleared the
// note. The conflicted sync had already advanced the base to canonical
// head, so the very next push was a fast-forward that republished the
// pre-merge tree — reverting upstream's work with exit 0 and no message.
func TestStashingAConflictedSyncDoesNotCountAsResolving(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("stash-escape")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync output", conflicted.combined(), "have conflicts")

	// The escape: the markers go into the stash, the worktree comes back
	// to HEAD, and HEAD has not moved.
	ws.git("stash", "push", "--quiet", "--", "docs")
	requireNotContains(t, "docs/api.md after stashing", ws.readDocs("api.md"), "<<<<<<<")

	push := ws.push()
	requireExit(t, "push after stashing the conflict away", push, 1)
	requireContains(t, "rejection", push.combined(), "was never resolved by a commit")
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")
	if !fileExists(t, ws.path(".git", "sanho", "sync.json")) {
		t.Error("the stash cleared the sync note; a sync is still owed and nothing records it")
	}
	// Upstream's line survived: nothing was published.
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")
}

// TestAnUnresolvedSyncDoesNotBlockUnrelatedCommits pins the pre-commit
// half of X1's semantics.
//
// The state is informational on the commit path (P2): a sync that was
// stashed away is not a reason to stop unrelated work, and the push
// boundary is where it is stopped. The notice appears every time, and
// committing other work keeps working — including a commit that moves
// HEAD, which must NOT by itself count as the missing resolution.
func TestAnUnresolvedSyncDoesNotBlockUnrelatedCommits(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("stash-then-commit")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")
	ws.git("stash", "push", "--quiet", "--", "docs")

	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "-A")
	commit := ws.gitExit("commit", "-m", "feat: unrelated code")
	requireExit(t, "an unrelated commit while a sync is owed", commit, 0)
	requireContains(t, "pre-commit notice", commit.combined(), "was never resolved by a commit")

	// HEAD moved, but nothing resolved the sync: the note stands and the
	// push is still refused.
	if !fileExists(t, ws.path(".git", "sanho", "sync.json")) {
		t.Fatal("an unrelated commit cleared the sync note")
	}
	push := ws.push()
	requireExit(t, "push after an unrelated commit", push, 1)
	requireContains(t, "rejection", push.combined(), "was never resolved by a commit")
}

// TestAbortAfterAStashLeavesAnHonestConflict follows the advice through.
//
// `sanho sync --abort` restores the pre-sync state; popping the stash
// brings the user's own markers back into the worktree (they are not
// committed, so the push gate does not see them); and the push is then
// refused as the ordinary case-③ conflict it always was — with the
// conflicted file named (X4).
func TestAbortAfterAStashLeavesAnHonestConflict(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("stash-abort")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")
	ws.git("stash", "push", "--quiet", "--", "docs")

	abort := ws.run("sync", "--abort")
	requireExit(t, "abort in the stashed state", abort, 0)
	if fileExists(t, ws.path(".git", "sanho", "sync.json")) {
		t.Error("abort left the sync note behind")
	}
	requireEqual(t, "docs/api.md after abort", ws.readDocs("api.md"), "line one\nMINE\n")

	ws.git("stash", "pop")
	requireContains(t, "docs/api.md after popping", ws.readDocs("api.md"), "<<<<<<<")

	push := ws.push()
	requireExit(t, "push after aborting", push, 1)
	requireContains(t, "rejection", push.combined(), "your docs changes conflict with upstream")
	requireContains(t, "rejection", push.combined(), "\n  docs/api.md\n")
}

// TestCommittedResolutionStillClearsTheNote is the unchanged happy path:
// the fix must not make an actual resolution stop counting.
func TestCommittedResolutionStillClearsTheNote(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("resolve")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")
	resolve := ws.gitExit("commit", "-m", "docs: resolve the conflict")
	requireExit(t, "the resolution commit", resolve, 0)

	push := ws.push()
	requireExit(t, "push after resolving", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	if fileExists(t, ws.path(".git", "sanho", "sync.json")) {
		t.Error("the publishing push left the sync note behind")
	}
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
}

// TestRestoringDocsFromTheIndexStillHitsTheMarkerGate keeps the other
// escape route unchanged: `git restore docs/` re-materializes the merged
// content from the index, so the markers are still there and the
// existing gate is what speaks.
func TestRestoringDocsFromTheIndexStillHitsTheMarkerGate(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("restore-path")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	ws.git("restore", "--", "docs")
	requireContains(t, "docs/api.md after restore", ws.readDocs("api.md"), "<<<<<<<")

	push := ws.push()
	requireExit(t, "push with the markers still in the worktree", push, 1)
	requireContains(t, "rejection", push.combined(), "finish the sync first")
}

// --- X2: abort is structurally infallible -------------------------------

// TestAbortSucceedsWithACorruptSyncNote is the reviewer's second finding.
//
// A `sync.json` that cannot be parsed made every path report a raw
// parse error, including `sanho sync --abort` — the one command whose
// contract is that it cannot fail once a note exists. The workspace was
// then stuck: markers in docs/, a note nothing could read, and no
// command that could clear either.
func TestAbortSucceedsWithACorruptSyncNote(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("corrupt-note")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")
	requireContains(t, "docs/api.md", ws.readDocs("api.md"), "<<<<<<<")

	writeFile(t, ws.path(".git", "sanho", "sync.json"), "{ this file is not JSON\n")

	// Every reader routes to the abort rather than printing the parse
	// error: `sanho sync`, `sanho doctor`, and the push boundary.
	blocked := ws.run("sync")
	requireExit(t, "sync with a corrupt note", blocked, 1)
	requireContains(t, "sync guidance", blocked.combined(), "record of the sync in progress is unreadable")
	requireContains(t, "sync guidance", blocked.combined(), "sanho sync --abort")

	doctor := ws.run("doctor")
	requireExit(t, "doctor with a corrupt note", doctor, 0)
	requireContains(t, "doctor", doctor.combined(), "record of the sync in progress is unreadable")

	push := ws.push()
	requireExit(t, "push with a corrupt note", push, 1)
	requireContains(t, "push rejection", push.combined(), "record of the sync in progress is unreadable")

	// The advised abort works, degraded: docs restored, note gone,
	// and the base left where the sync put it with `doctor --fix` named.
	abort := ws.run("sync", "--abort")
	requireExit(t, "abort with a corrupt note", abort, 0)
	requireContains(t, "abort output", abort.combined(), "sanho doctor --fix")
	if fileExists(t, ws.path(".git", "sanho", "sync.json")) {
		t.Error("abort left the corrupt sync note behind")
	}
	requireEqual(t, "docs/api.md after abort", ws.readDocs("api.md"), "line one\nMINE\n")
}

// TestACorruptSyncNoteDoesNotBreakCommits is the P2 half of X2.
//
// A file sanho itself cannot read is sanho's problem, and the commit
// path is exactly where Critical C1 said an internal problem must never
// surface as a blocked commit. The notice appears, the commit lands.
//
// The note is corrupted in a workspace with no markers anywhere, so the
// commit's own content is not what is being tested: the §5.6 gates are
// unaffected and still block staged markers, as their own tests pin.
func TestACorruptSyncNoteDoesNotBreakCommits(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("corrupt-note-commit")

	writeFile(t, ws.path(".git", "sanho", "sync.json"), "{ this file is not JSON\n")

	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "src/main.go")
	commit := ws.gitExit("commit", "-m", "feat: unrelated code")
	requireExit(t, "an unrelated commit with a corrupt note", commit, 0)
	requireContains(t, "pre-commit notice", commit.combined(), "record of the sync in progress is unreadable")
}

// --- X3: the marker gate's baseline is canonical ------------------------

// TestPushGateScansAgainstCanonicalNotTheRemoteTip is the third finding.
//
// The gate scoped its scan to the diff against the *app remote's*
// previous tip, which a single `git push --no-verify` poisons: the
// markers reach the code remote without ever passing the gate, and every
// later push then treats them as already-vetted history. Canonical head
// is the only baseline the induction actually holds for.
func TestPushGateScansAgainstCanonicalNotTheRemoteTip(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("no-verify-history")

	ws.writeDocs(map[string]string{"bad.md": markerDoc})
	ws.git("add", "-A")
	ws.git("commit", "--no-verify", "-m", "docs: markers slipped in")
	bypass := ws.gitExit("push", "--no-verify", "--quiet", "origin", "main")
	requireExit(t, "push --no-verify", bypass, 0)

	// An entirely unrelated docs change, pushed normally.
	ws.commitDocs("docs: an unrelated note", map[string]string{"guide.md": "clean\n"})

	rejected := ws.push()
	requireExit(t, "push over a --no-verify marker history", rejected, 1)
	requireContains(t, "rejection", rejected.combined(), "pushed docs still contain conflict markers")
	requireContains(t, "rejection", rejected.combined(), "docs/bad.md")

	for _, path := range w.canonicalPaths(w.canonicalHead()) {
		if path == "bad.md" {
			t.Fatal("canonical published a file carrying conflict markers")
		}
	}
}

// TestPushGateStillPassesForCleanDocs is the other half: the widened
// baseline must not start rejecting ordinary pushes.
func TestPushGateStillPassesForCleanDocs(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("clean-gate")

	ws.commitDocs("docs: a clean note", map[string]string{"guide.md": "clean\n"})
	push := ws.push()
	requireExit(t, "an ordinary push", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
}

// --- X4: the conflict rejection names the files -------------------------

// TestPushConflictRejectionNamesTheConflictedFiles is the fourth
// finding: template 3 stated that a conflict existed and left the user
// to find it, while the very same rejection already carried the list.
func TestPushConflictRejectionNamesTheConflictedFiles(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("named-conflicts")

	ws.commitDocs("docs: my edits", map[string]string{
		"api.md":   "line one\nMINE\n",
		"guide.md": "guide one\nMINE\n",
	})
	push := ws.push()
	requireExit(t, "seed push", push, 0)

	ws.commitDocs("docs: more of my edits", map[string]string{
		"api.md":   "line one\nMINE AGAIN\n",
		"guide.md": "guide one\nMINE AGAIN\n",
	})
	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nTHEIRS\n",
		"guide.md": "guide one\nTHEIRS\n",
	}, "canonical: their edits")

	rejected := ws.push()
	requireExit(t, "conflicting push", rejected, 1)
	requireContains(t, "rejection", rejected.combined(), "your docs changes conflict with upstream")
	requireContains(t, "rejection", rejected.combined(), "\n  docs/api.md\n")
	requireContains(t, "rejection", rejected.combined(), "\n  docs/guide.md\n")
	requireContains(t, "rejection", rejected.combined(), "Run 'sanho sync', resolve, commit, then push again.")
}

// --- X5: doctor reports a base history disagrees with -------------------

// TestDoctorReportsABaseHistoryDisagreesWith is the fifth finding.
//
// §5.10 withholds base re-derivation whenever the docs worktree differs
// from HEAD's, and derive.go promises that `sanho doctor` flags the
// resulting inconsistency. Nothing did.
func TestDoctorReportsABaseHistoryDisagreesWith(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("base-drift")
	original := w.canonicalHead()

	advanced := w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	ws.sanho("sync")

	// Regress the base file behind what the commit trailers record.
	writeFile(t, ws.basePath(), fmt.Sprintf("{\n  \"version\": 2,\n  \"commit\": %q,\n  \"tree\": \"\"\n}\n", original))

	report := ws.run("doctor")
	requireExit(t, "doctor", report, 0)
	requireContains(t, "doctor", report.combined(), "base-derivation")
	requireContains(t, "doctor", report.combined(), "re-derive the base from commit history")

	fixed := ws.run("doctor", "--fix")
	requireExit(t, "doctor --fix", fixed, 0)
	requireContains(t, "base file after --fix", readFile(t, ws.basePath()), advanced)
}

// TestDoctorTreatsAWithheldRederivationAsInformational is the dirty-docs
// half: re-derivation is deliberately withheld there (§5.10 step 1), so
// the disagreement is a fact to report, not a problem to warn about.
func TestDoctorTreatsAWithheldRederivationAsInformational(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("base-drift-dirty")
	original := w.canonicalHead()

	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	ws.sanho("sync")

	writeFile(t, ws.basePath(), fmt.Sprintf("{\n  \"version\": 2,\n  \"commit\": %q,\n  \"tree\": \"\"\n}\n", original))
	ws.writeDocs(map[string]string{"api.md": "line one\nwork in progress\n"})

	var document struct {
		Checks []struct {
			Name     string `json:"name"`
			Severity string `json:"severity"`
		} `json:"checks"`
		Warnings int `json:"warnings"`
	}
	report := ws.run("doctor", "--json")
	requireExit(t, "doctor --json", report, 0)
	if err := json.Unmarshal([]byte(report.stdout), &document); err != nil {
		t.Fatalf("parse doctor JSON: %v\n%s", err, report.stdout)
	}

	found := false
	for _, check := range document.Checks {
		if check.Name != "base-derivation" {
			continue
		}
		found = true
		if check.Severity != "info" {
			t.Errorf("base-derivation severity = %q, want info while the docs are dirty", check.Severity)
		}
	}
	if !found {
		t.Fatalf("doctor reported no base-derivation check:\n%s", report.stdout)
	}
}

// TestDoctorIsSilentAboutABaseAPublicationAdvanced is the other half of
// X5, and the reason the check is not a bare inequality.
//
// Publication's base-advance rule moves the base past the commit the
// trailers name, so a workspace that has just pushed always disagrees
// with its own history. Warning there would fire on the happiest path
// and `--fix` would write the OLDER base back — a repair that regresses
// the state it claims to repair.
func TestDoctorIsSilentAboutABaseAPublicationAdvanced(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("published")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	push := ws.push()
	requireExit(t, "push", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")

	report := ws.run("doctor")
	requireExit(t, "doctor after publishing", report, 0)
	requireNotContains(t, "doctor", report.combined(), "base-derivation")
	requireContains(t, "doctor", report.combined(), "no problems found")
}

// --- m1: the cheapest rejection comes first -----------------------------

// TestSyncNoteRejectionPrecedesTheClone pins the §5.3 ordering
// principle: the sync gate needs one local file, so a push it refuses
// must not first create and fetch a canonical clone.
func TestSyncNoteRejectionPrecedesTheClone(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("cheap-refusal")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	if err := os.RemoveAll(ws.cloneDir()); err != nil {
		t.Fatalf("remove the clone: %v", err)
	}

	push := ws.push()
	requireExit(t, "push during a sync with no clone", push, 1)
	requireContains(t, "rejection", push.combined(), "finish the sync first")
	if fileExists(t, ws.cloneDir()) {
		t.Error("a push refused on a local file still created the canonical clone")
	}
}

// --- m4: the JSON error envelope and the post-migrate flow --------------

// TestJSONErrorEnvelopeCoversTheRepresentativeCodes widens the §5.8
// envelope coverage past not_in_workspace: an agent branches on `code`,
// so each code an ordinary session can hit needs a test that produces it.
func TestJSONErrorEnvelopeCoversTheRepresentativeCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		args []string
		// reach puts the workspace into the state, and returns nothing:
		// the command under test is `args`.
		reach func(t *testing.T, w *world, ws *workspace)
	}{
		{
			name: "sync_required",
			code: "sync_required",
			args: []string{"pull", "--json"},
			reach: func(t *testing.T, w *world, ws *workspace) {
				ws.writeDocs(map[string]string{"api.md": "line one\nstaged\n"})
				ws.git("add", "docs/api.md")
			},
		},
		{
			name: "sync_in_progress",
			code: "sync_in_progress",
			args: []string{"sync", "--json"},
			reach: func(t *testing.T, w *world, ws *workspace) {
				ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
				w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
				ws.sanho("sync")
			},
		},
		{
			name: "canonical_unreachable",
			code: "canonical_unreachable",
			args: []string{"sync", "--json"},
			reach: func(t *testing.T, w *world, ws *workspace) {
				ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
				w.takeCanonicalOffline()
			},
		},
		{
			name: "docs_dirty",
			code: "docs_dirty",
			args: []string{"sync", "--json"},
			reach: func(t *testing.T, w *world, ws *workspace) {
				ws.writeDocs(map[string]string{"api.md": "line one\nuncommitted\n"})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, defaultCanonicalDocs())
			ws := w.setup(test.name)
			test.reach(t, w, ws)

			res := ws.run(test.args...)
			requireExit(t, "sanho "+strings.Join(test.args, " "), res, 1)

			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(res.stdout), &envelope); err != nil {
				t.Fatalf("stdout is not a JSON envelope: %v\n%s", err, res.stdout)
			}
			if envelope.Error.Code != test.code {
				t.Errorf("error code = %q, want %q\n%s", envelope.Error.Code, test.code, res.stdout)
			}
			if envelope.Error.Message == "" {
				t.Error("the envelope carries no message")
			}
			if strings.TrimSpace(res.stderr) == "" {
				t.Error("the prose half of the failure never reached stderr")
			}
		})
	}
}

// TestMigratedWorkspaceSyncsPullsAndPushes is the missing end-to-end
// link: §8 promises a v0.1 workspace becomes an ordinary v0.2 one, and
// nothing proved the three flows actually run afterwards.
func TestMigratedWorkspaceSyncsPullsAndPushes(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.newWorkspace("migrated")
	base := w.canonicalHead()

	ws.writeDocs(map[string]string{"api.md": "line one\nline two\n"})
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: adopt canonical\n\ndocs-version: "+base)
	seedV1Workspace(t, ws, base, true)

	migrate := ws.run("migrate")
	requireExit(t, "migrate", migrate, 0)
	requireContains(t, "config", readFile(t, ws.path(".sanho.json")), `"schema_version": 2`)

	// 1. sync adopts upstream's new document.
	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	sync := ws.run("sync")
	requireExit(t, "sync after migrate", sync, 0)
	requireEqual(t, "docs/guide.md", ws.readDocs("guide.md"), "upstream guide\n")

	// 2. pull is a no-op fast-forward from there.
	pull := ws.run("pull")
	requireExit(t, "pull after migrate", pull, 0)
	requireContains(t, "pull output", pull.combined(), "up to date")

	// 3. and a local docs commit publishes.
	ws.commitDocs("docs: post-migration edit", map[string]string{"api.md": "line one\nmigrated\n"})
	push := ws.push()
	requireExit(t, "push after migrate", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nmigrated\n")
}
