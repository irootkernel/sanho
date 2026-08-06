package e2e

// The guidance-closure suite (sanho-v0.2.md §5.9, §9 rule 2).
//
// D3 is normative: *every advised command must succeed in the state
// where it is advised*. Audit H5 was that rule being violated — messages
// naming commands that failed where they were printed — so this suite
// does not sample. For every entry in cli.Catalog it
//
//  1. builds a real workspace in exactly the advising state,
//  2. runs the binary until the message actually appears (asserted, so a
//     fixture that stopped reproducing the state fails loudly rather
//     than passing vacuously),
//  3. runs each advised command verbatim through /bin/sh, in that state,
//     and requires success.
//
// Each advised command gets its own world. "In that state" is the whole
// claim; running a second command after the first one has already
// changed the state would prove something weaker.
//
// Two entries name a command the message spells in prose — push_markers
// ("resolve the markers before pushing") and canonical_unreachable
// ("Check network access …, then push again"). Their advised action is
// the *retry*, so their fixtures perform the human half of the advice
// (resolve; restore the URL) in `prepare` and then run `git push`. That
// is the honest reading of an environmental message: the tool's promise
// is that the push works once the named condition is cleared.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/interface/cli"
)

// closureState is what a fixture hands back: the state, the output that
// must carry the message, and how to run the commands the catalog says
// that message advises.
type closureState struct {
	// ws is the workspace the advised commands run in.
	ws *workspace
	// output is everything the binary (or git) printed on reaching the
	// advising state.
	output string
	// substitutions expand the catalog's angle-bracketed placeholders.
	substitutions map[string]string
	// runAs overrides how a catalog command is invoked, for the one
	// message that names a command without the arguments it needs
	// (`sanho init`). The override still has to be the same command.
	runAs map[string]string
	// prepare is the human half of the advice, for messages whose action
	// line is prose ("resolve the markers", "check network access").
	prepare map[string]func(t *testing.T, ws *workspace)
	// verify runs after a command succeeded, to prove the state really
	// moved rather than the command merely exiting 0.
	verify map[string]func(t *testing.T, ws *workspace)
}

// closureFixture builds one scenario.
type closureFixture func(t *testing.T, w *world) closureState

// TestGuidanceClosure is the §9 rule 2 table. The manifest cross-check
// runs first: a catalog entry with no fixture, or a fixture for a
// scenario the catalog dropped, fails the build rather than silently
// reducing coverage.
func TestGuidanceClosure(t *testing.T) {
	t.Parallel()
	requireFixtureForEveryScenario(t)

	for _, entry := range cli.Catalog {
		t.Run(entry.Scenario, func(t *testing.T) {
			t.Parallel()
			for _, command := range entry.NextCommands {
				t.Run(commandLabel(command), func(t *testing.T) {
					t.Parallel()
					runClosureCase(t, entry, command)
				})
			}
		})
	}
}

// runClosureCase is the closure contract for one (message, command)
// pair, in a world of its own.
func runClosureCase(t *testing.T, entry cli.CatalogEntry, command string) {
	t.Helper()

	w := newWorld(t, defaultCanonicalDocs())
	state := closureFixtures[entry.Scenario](t, w)

	// 1. The message really appears. Without this the rest proves
	//    nothing: a fixture that drifted out of the advising state would
	//    happily run a command that succeeds for unrelated reasons.
	if !strings.Contains(state.output, entry.Match) {
		t.Fatalf("scenario %q did not produce message %q.\nwanted substring: %q\ngot:\n%s",
			entry.Scenario, entry.ID, entry.Match, state.output)
	}

	// 2. The message really names this command. Only claimed when the
	//    catalog's own rendering spells it: two entries advise a retry
	//    the message describes in prose (see the file comment).
	resolved := substitute(command, state.substitutions)
	if strings.Contains(entry.Sample, command) &&
		!strings.Contains(state.output, command) &&
		!strings.Contains(state.output, resolved) {
		t.Fatalf("scenario %q printed a message that does not name %q:\n%s",
			entry.Scenario, resolved, state.output)
	}

	// 3. The human half of the advice, where the message states one.
	if prepare := state.prepare[command]; prepare != nil {
		prepare(t, state.ws)
	}

	// 4. The advised command, verbatim, in the advising state.
	invocation := resolved
	if override, ok := state.runAs[command]; ok {
		invocation = substitute(override, state.substitutions)
	}
	res := state.ws.shell(invocation)
	if res.exitCode != 0 {
		t.Fatalf("GUIDANCE CLOSURE FAILURE (%s / %s)\n"+
			"the message advised: %s\n"+
			"running it in the state that printed it exited %d\n"+
			"--- advising message ---\n%s\n"+
			"--- command output ---\n%s\n%s",
			entry.ID, entry.Scenario, invocation, res.exitCode,
			state.output, res.stdout, res.stderr)
	}

	// 5. And it did what it was named for.
	if verify := state.verify[command]; verify != nil {
		verify(t, state.ws)
	}
}

// requireFixtureForEveryScenario compares the catalog manifest against
// this file's table, in both directions.
func requireFixtureForEveryScenario(t *testing.T) {
	t.Helper()

	built := make([]string, 0, len(closureFixtures))
	for scenario := range closureFixtures {
		built = append(built, scenario)
	}
	sort.Strings(built)

	manifest := cli.ClosureScenarios()
	if strings.Join(built, ",") == strings.Join(manifest, ",") {
		return
	}
	for _, scenario := range manifest {
		if closureFixtures[scenario] == nil {
			t.Errorf("catalog scenario %q has no closure fixture; every advising message needs one", scenario)
		}
	}
	inManifest := map[string]bool{}
	for _, scenario := range manifest {
		inManifest[scenario] = true
	}
	for _, scenario := range built {
		if !inManifest[scenario] {
			t.Errorf("closure fixture %q matches no catalog scenario", scenario)
		}
	}
}

// substitute expands the catalog's angle-bracketed placeholders.
func substitute(command string, substitutions map[string]string) string {
	for placeholder, value := range substitutions {
		command = strings.ReplaceAll(command, placeholder, value)
	}
	return command
}

// commandLabel turns a command line into a readable subtest name.
func commandLabel(command string) string {
	label := strings.NewReplacer(" ", "_", "/", "", "&", "", "<", "", ">", "", "-", "_").Replace(command)
	for strings.Contains(label, "__") {
		label = strings.ReplaceAll(label, "__", "_")
	}
	return strings.Trim(label, "_")
}

// defaultCanonicalDocs is the canonical seed most fixtures start from:
// two lines, so a same-file conflict is a one-hunk conflict.
func defaultCanonicalDocs() map[string]string {
	return map[string]string{"api.md": "line one\nline two\n"}
}

// --- the table --------------------------------------------------------

var closureFixtures = map[string]closureFixture{
	"v1_layout":              reachV1Layout,
	"not_a_workspace":        reachNotAWorkspace,
	"push_sync_in_progress":  reachPushSyncInProgress,
	"clean_unconfirmed":      reachCleanUnconfirmed,
	"clean_sync_in_progress": reachCleanSyncInProgress,
	"migrate_blocked":        reachMigrateBlocked,
	"behind_clean":           reachBehindClean,
	"behind_conflicts":       reachBehindConflicts,
	"behind_unknown":         reachBehindUnknown,
	"sync_conflict":          reachSyncConflict,
	"unresolved_sync":        reachUnresolvedSync,
	"staged_markers":         reachStagedMarkers,
	"push_conflict":          reachPushConflict,
	"push_sync_required":     reachPushSyncRequired,
	"push_markers":           reachPushMarkers,
	"canonical_unreachable":  reachCanonicalUnreachable,
	"history_rewritten":      reachHistoryRewritten,
	"stamp_warning":          reachStampWarning,
	"doctor_fix_hint":        reachDoctorFixHint,
	"stale_data":             reachStaleData,
	"never_fetched":          reachNeverFetched,
}

// --- fixtures ---------------------------------------------------------

// reachV1Layout is §8's pre-migration degradation: the v0.2 binary is
// installed, `migrate` has not run, and the push boundary is where the
// migration is demanded.
func reachV1Layout(t *testing.T, w *world) closureState {
	ws := w.newWorkspace("v1")
	base := w.canonicalHead()

	ws.writeDocs(map[string]string{"api.md": "line one\nline two\n"})
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: adopt canonical\n\ndocs-version: "+base)
	seedV1Workspace(t, ws, base, true)

	return closureState{ws: ws, output: ws.push().combined()}
}

// reachNotAWorkspace runs a read command where there is no `.sanho.json`.
func reachNotAWorkspace(t *testing.T, w *world) closureState {
	ws := w.newWorkspace("bare")
	out := ws.run("status")
	requireExit(t, "status outside a workspace", out, 1)

	return closureState{
		ws:     ws,
		output: out.combined(),
		// The message names the command; the flags are the ones `sanho
		// init` itself requires, and naming them here is what makes the
		// advice runnable rather than a shape.
		runAs: map[string]string{
			"sanho init": fmt.Sprintf("sanho init --project %s --docs-repo-url %s --actor-email %s",
				projectName, w.origin, actorEmail),
		},
		verify: map[string]func(*testing.T, *workspace){
			"sanho init": func(t *testing.T, ws *workspace) {
				if !fileExists(t, ws.path(".sanho.json")) {
					t.Error("sanho init left no workspace config behind")
				}
			},
		},
	}
}

// reachPushSyncInProgress is §5.3 step 2: a push while a conflicted sync
// still owns the docs worktree.
func reachPushSyncInProgress(t *testing.T, w *world) closureState {
	ws := conflictedSync(t, w)
	return closureState{
		ws:     ws,
		output: ws.push().combined(),
		verify: syncNoteCleared(),
	}
}

func reachCleanUnconfirmed(t *testing.T, w *world) closureState {
	ws := w.setup("clean")
	out := ws.run("clean")
	requireExit(t, "clean without -y", out, 1)
	return closureState{ws: ws, output: out.combined()}
}

func reachCleanSyncInProgress(t *testing.T, w *world) closureState {
	ws := conflictedSync(t, w)
	out := ws.run("clean", "-y")
	requireExit(t, "clean during a sync", out, 1)
	return closureState{ws: ws, output: out.combined(), verify: syncNoteCleared()}
}

// reachMigrateBlocked is §8 step 1: a live v0.1 transaction directory.
func reachMigrateBlocked(t *testing.T, w *world) closureState {
	ws := w.newWorkspace("blocked")
	base := w.canonicalHead()

	ws.writeDocs(map[string]string{"api.md": "line one\nline two\n"})
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: adopt canonical\n\ndocs-version: "+base)
	seedV1Workspace(t, ws, base, true)
	mkdirAll(t, ws.path(".git", "sanho", "pull-commit"))

	out := ws.run("migrate")
	requireExit(t, "migrate with a live transaction", out, 1)

	return closureState{
		ws:     ws,
		output: out.combined(),
		prepare: map[string]func(*testing.T, *workspace){
			// The message says to finish or abort the transaction with
			// the v0.1 binary first; removing its directory is that step
			// as this fixture can express it.
			"sanho migrate": func(t *testing.T, ws *workspace) {
				if err := os.RemoveAll(ws.path(".git", "sanho", "pull-commit")); err != nil {
					t.Fatalf("clear the v0.1 transaction: %v", err)
				}
			},
		},
		verify: map[string]func(*testing.T, *workspace){
			"sanho migrate": func(t *testing.T, ws *workspace) {
				requireContains(t, "config", readFile(t, ws.path(".sanho.json")), `"schema_version": 2`)
			},
		},
	}
}

// reachBehindClean is the §5.9 template-1 warning: the base is behind and
// the merge would be clean.
func reachBehindClean(t *testing.T, w *world) closureState {
	ws := w.setup("behind")
	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	ws.sanho("status", "--refresh")

	return closureState{ws: ws, output: commitSomeCode(t, ws), verify: docsUpToDate()}
}

// reachBehindConflicts is the same warning's conflict-prediction variant,
// which §5.9 requires to say what happens if it is ignored.
func reachBehindConflicts(t *testing.T, w *world) closureState {
	ws := w.setup("behind-conflict")
	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("status", "--refresh")

	return closureState{
		ws:     ws,
		output: commitSomeCode(t, ws),
		verify: map[string]func(*testing.T, *workspace){
			// A conflicted sync is a success (§5.5 step 6): it did what
			// it was asked to, and the markers are now the user's work.
			"sanho sync": func(t *testing.T, ws *workspace) {
				requireContains(t, "docs after sync", ws.readDocs("api.md"), "<<<<<<< sanho-ours")
			},
		},
	}
}

// reachBehindUnknown is the degraded form of the same warning: behind,
// but the clean/conflict prediction could not be computed.
//
// The fixture freezes the private clone, which is the state that makes
// the prediction impossible — the preview must import the app tip into
// the clone, and a clone it cannot write to cannot take it. The freeze is
// transient by construction (it is lifted before the advised command
// runs), because that is what the state is: a temporary inability to
// predict, not a permanently broken workspace. Nothing about the base
// being behind changes, which is what the advice is about.
func reachBehindUnknown(t *testing.T, w *world) closureState {
	ws := w.setup("unknown")
	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	ws.sanho("status", "--refresh")

	ws.freezeClone()
	output := commitSomeCode(t, ws)

	return closureState{
		ws:     ws,
		output: output,
		prepare: map[string]func(*testing.T, *workspace){
			"sanho sync": func(t *testing.T, ws *workspace) {
				chmodTree(t, ws.cloneDir(), 0o700, 0o600)
			},
		},
		verify: docsUpToDate(),
	}
}

// reachSyncConflict is §5.9 template 2, produced by `sanho sync` itself.
func reachSyncConflict(t *testing.T, w *world) closureState {
	ws := w.setup("sync-conflict")
	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	out := ws.sanho("sync")
	return closureState{
		ws:      ws,
		output:  out.combined(),
		prepare: resolveMarkersFirst(),
		verify:  resolutionOutcomes(),
	}
}

// reachUnresolvedSync is the same two next steps printed by the commit
// that tries to land an unresolved sync (§5.6).
func reachUnresolvedSync(t *testing.T, w *world) closureState {
	ws := conflictedSync(t, w)
	ws.git("add", "docs/api.md")

	blocked := ws.gitExit("commit", "-m", "docs: resolve")
	requireExit(t, "commit with an unresolved sync", blocked, 1)

	return closureState{
		ws:      ws,
		output:  blocked.combined(),
		prepare: resolveMarkersFirst(),
		verify:  resolutionOutcomes(),
	}
}

// reachStagedMarkers is the staged-marker gate outside any sync (§5.6
// step 1): markers sanho did not put there are blocked just the same.
func reachStagedMarkers(t *testing.T, w *world) closureState {
	ws := w.setup("staged-markers")
	ws.writeDocs(map[string]string{
		"api.md": "<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n",
	})
	ws.git("add", "docs/api.md")

	blocked := ws.gitExit("commit", "-m", "docs: oops")
	requireExit(t, "commit with staged markers", blocked, 1)

	return closureState{
		ws:      ws,
		output:  blocked.combined(),
		prepare: resolveMarkersFirst(),
		verify: map[string]func(*testing.T, *workspace){
			"git add docs/ && git commit": func(t *testing.T, ws *workspace) {
				requireEqual(t, "docs/api.md", ws.readDocs("api.md"), "line one\nRESOLVED\n")
			},
		},
	}
}

// reachPushConflict is §5.9 template 3, the case ③-conflict rejection.
func reachPushConflict(t *testing.T, w *world) closureState {
	ws := w.setup("push-conflict")
	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	push := ws.push()
	requireExit(t, "conflicting push", push, 1)

	return closureState{
		ws:     ws,
		output: push.combined(),
		verify: map[string]func(*testing.T, *workspace){
			// The message says "run sanho sync, resolve, commit, then
			// push again" — so the whole sentence is what gets proven,
			// not only its first verb.
			"sanho sync": func(t *testing.T, ws *workspace) {
				requireContains(t, "docs after sync", ws.readDocs("api.md"), "<<<<<<< sanho-ours")
				ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
				ws.git("add", "docs/api.md")
				ws.git("commit", "-m", "docs: resolve the conflict")

				final := ws.push()
				requireExit(t, "push after the advised sync", final, 0)
				requireContains(t, "push output", final.combined(), "published docs")
				requireEqual(t, "canonical api.md",
					ws.w.canonicalFile(ws.w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
			},
		},
	}
}

// reachPushSyncRequired is the no-base rejection: nothing records what
// these docs derive from, so there is no merge base to publish against.
func reachPushSyncRequired(t *testing.T, w *world) closureState {
	ws := w.setup("no-base")
	removeFile(t, ws.basePath())
	ws.commitDocs("docs: local edit", map[string]string{"guide.md": "local guide\n"})

	push := ws.push()
	requireExit(t, "push with no recorded base", push, 1)

	return closureState{
		ws:     ws,
		output: push.combined(),
		verify: map[string]func(*testing.T, *workspace){
			"sanho sync": func(t *testing.T, ws *workspace) {
				if !fileExists(t, ws.basePath()) {
					t.Error("the advised sync established no base")
				}
			},
		},
	}
}

// reachPushMarkers is §5.3 step 3: committed markers must never reach
// canonical. The commit itself needs --no-verify, because §5.6's gate
// already refuses to create it — which is the point.
func reachPushMarkers(t *testing.T, w *world) closureState {
	ws := w.setup("push-markers")
	ws.writeDocs(map[string]string{
		"api.md": "<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n",
	})
	ws.git("add", "-A")
	ws.git("commit", "--no-verify", "-m", "docs: markers slipped in")

	push := ws.push()
	requireExit(t, "push with committed markers", push, 1)

	return closureState{
		ws:     ws,
		output: push.combined(),
		prepare: map[string]func(*testing.T, *workspace){
			// "resolve the markers before pushing" — the message's own
			// words, performed here so the advised retry can be run.
			"git push": func(t *testing.T, ws *workspace) {
				ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
				ws.git("add", "docs/api.md")
				ws.git("commit", "-m", "docs: resolve the markers")
			},
		},
		verify: map[string]func(*testing.T, *workspace){
			"git push": func(t *testing.T, ws *workspace) {
				requireEqual(t, "canonical api.md",
					ws.w.canonicalFile(ws.w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
			},
		},
	}
}

// reachCanonicalUnreachable is the §5.2 fail-closed write path.
func reachCanonicalUnreachable(t *testing.T, w *world) closureState {
	ws := w.setup("offline")
	ws.commitDocs("docs: local edit", map[string]string{"api.md": "line one\nmine\n"})
	w.takeCanonicalOffline()

	push := ws.push()
	requireExit(t, "push with canonical unreachable", push, 1)

	return closureState{
		ws:     ws,
		output: push.combined(),
		prepare: map[string]func(*testing.T, *workspace){
			// "Check network access to the docs repository, then push
			// again": the advised action is the retry, so the fixture
			// restores access and runs exactly that.
			"git push": func(t *testing.T, ws *workspace) { ws.w.bringCanonicalOnline() },
		},
		verify: map[string]func(*testing.T, *workspace){
			"git push": func(t *testing.T, ws *workspace) {
				requireEqual(t, "canonical api.md",
					ws.w.canonicalFile(ws.w.canonicalHead(), "api.md"), "line one\nmine\n")
			},
		},
	}
}

// reachHistoryRewritten is §5.3 case ④ with no re-anchor available:
// canonical history was replaced wholesale, so neither the recorded base
// commit nor its docs tree survives.
//
// KNOWN PRODUCT DEFECT — the `sanho sync --rebase-onto <commit>` half of
// this row fails, and the assertion is deliberately left at full
// strength. `usecase/docsync.resolveBaseTree` refuses whenever the
// recorded base is unreachable *and* no canonical commit carries its
// docs-base-tree — which is precisely the state that produces this
// message — so `--rebase-onto` cannot escape the state its own guidance
// names, and the error it prints advises the very command that just
// failed. Fixing it means letting an explicit --rebase-onto target
// stand in for the failed re-anchor (a flow change in usecase/docsync),
// not a wording change, so it is reported rather than patched here.
func reachHistoryRewritten(t *testing.T, w *world) closureState {
	ws := w.setup("rewritten")
	ws.commitDocs("docs: local edit", map[string]string{"api.md": "line one\nmine\n"})

	rewritten := w.rewriteCanonical(
		map[string]string{"handbook.md": "an entirely new canonical\n"},
		"canonical: rewritten history", true)

	push := ws.push()
	requireExit(t, "push after a canonical rewrite", push, 1)

	return closureState{
		ws:     ws,
		output: push.combined(),
		substitutions: map[string]string{
			"<clone-dir>": ws.cloneDir(),
			"<branch>":    "main",
			"<commit>":    rewritten,
		},
	}
}

// reachStampWarning is §5.1's never-blocking commit-msg warning: the
// commit lands, unstamped, and names the local repair.
func reachStampWarning(t *testing.T, w *world) closureState {
	ws := w.setup("stamp")
	removeFile(t, ws.basePath())

	ws.writeDocs(map[string]string{"api.md": "line one\nedited\n"})
	ws.git("add", "-A")
	commit := ws.gitExit("commit", "-m", "docs: edit with no base recorded")
	requireExit(t, "commit with no base recorded", commit, 0)

	return closureState{ws: ws, output: commit.combined(), verify: baseRestored()}
}

func reachDoctorFixHint(t *testing.T, w *world) closureState {
	ws := w.setup("doctor")
	ws.commitDocs("docs: local edit", map[string]string{"api.md": "line one\nedited\n"})
	removeFile(t, ws.basePath())

	return closureState{ws: ws, output: ws.sanho("doctor").combined(), verify: baseRestored()}
}

// reachStaleData is §5.2's degraded read: cached canonical facts older
// than the §5.6 threshold always say how old they are.
func reachStaleData(t *testing.T, w *world) closureState {
	ws := w.setup("stale")
	ws.staleFetchMarker(time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339Nano))

	return closureState{ws: ws, output: ws.sanho("status").combined(), verify: fetchMarkerFresh()}
}

func reachNeverFetched(t *testing.T, w *world) closureState {
	ws := w.setup("never-fetched")
	ws.removeFetchMarker()

	return closureState{ws: ws, output: ws.sanho("status").combined(), verify: fetchMarkerFresh()}
}

// --- shared fixture pieces --------------------------------------------

// conflictedSync leaves a workspace mid-sync with markers in docs/ and a
// sync note on disk — the one pending state v0.2 has (§6).
func conflictedSync(t *testing.T, w *world) *workspace {
	t.Helper()

	ws := w.setup("conflicted")
	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	out := ws.sanho("sync")
	requireContains(t, "sync output", out.combined(), "have conflicts")
	if !fileExists(t, ws.path(".git", "sanho", "sync.json")) {
		t.Fatal("a conflicted sync left no sync note")
	}
	return ws
}

// commitSomeCode makes an ordinary non-docs commit, which is what fires
// the §5.6 freshness warning without changing docs.
func commitSomeCode(t *testing.T, ws *workspace) string {
	t.Helper()

	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "-A")
	commit := ws.gitExit("commit", "-m", "feat: unrelated code")
	requireExit(t, "commit on a stale base", commit, 0)
	return commit.combined()
}

// resolveMarkersFirst performs "resolve the markers" for the two
// templates that ask for it before `git add … && git commit`.
func resolveMarkersFirst() map[string]func(*testing.T, *workspace) {
	return map[string]func(*testing.T, *workspace){
		"git add docs/ && git commit": func(t *testing.T, ws *workspace) {
			ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
		},
	}
}

// resolutionOutcomes is the documented outcome of each of template 2's
// two commands: the commit lands and clears the sync, or the abort puts
// everything back.
func resolutionOutcomes() map[string]func(*testing.T, *workspace) {
	return map[string]func(*testing.T, *workspace){
		"git add docs/ && git commit": func(t *testing.T, ws *workspace) {
			requireEqual(t, "docs/api.md", ws.readDocs("api.md"), "line one\nRESOLVED\n")

			// §5.5 step 6: the resolution is an ordinary commit and
			// nothing else must happen at commit time — v0.2 removed
			// post-commit outright. The note is therefore cleared by the
			// next hook that runs, which is the push that publishes the
			// resolved tree via case ②.
			push := ws.push()
			requireExit(t, "push after resolving", push, 0)
			requireContains(t, "push output", push.combined(), "published docs")
			if fileExists(t, ws.path(".git", "sanho", "sync.json")) {
				t.Error("the publishing push left the sync note behind")
			}
			requireEqual(t, "canonical api.md",
				ws.w.canonicalFile(ws.w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
		},
		"sanho sync --abort": func(t *testing.T, ws *workspace) {
			requireEqual(t, "docs/api.md", ws.readDocs("api.md"), "line one\nMINE\n")
			if fileExists(t, ws.path(".git", "sanho", "sync.json")) {
				t.Error("abort left the sync note behind")
			}
		},
	}
}

func syncNoteCleared() map[string]func(*testing.T, *workspace) {
	return map[string]func(*testing.T, *workspace){
		"sanho sync --abort": func(t *testing.T, ws *workspace) {
			if fileExists(t, ws.path(".git", "sanho", "sync.json")) {
				t.Error("abort left the sync note behind")
			}
		},
	}
}

func docsUpToDate() map[string]func(*testing.T, *workspace) {
	return map[string]func(*testing.T, *workspace){
		"sanho sync": func(t *testing.T, ws *workspace) {
			requireEqual(t, "docs/guide.md", ws.readDocs("guide.md"), "upstream guide\n")
		},
	}
}

func baseRestored() map[string]func(*testing.T, *workspace) {
	return map[string]func(*testing.T, *workspace){
		"sanho doctor --fix": func(t *testing.T, ws *workspace) {
			if !fileExists(t, ws.basePath()) {
				t.Fatal("doctor --fix wrote no base file")
			}
			requireContains(t, "base file", readFile(t, ws.basePath()), `"commit"`)
		},
	}
}

func fetchMarkerFresh() map[string]func(*testing.T, *workspace) {
	return map[string]func(*testing.T, *workspace){
		"sanho status --refresh": func(t *testing.T, ws *workspace) {
			requireContains(t, "status after refresh",
				ws.sanho("status").stdout, "canonical data is")
		},
	}
}

// --- the v0.1 fixture --------------------------------------------------

// legacyHookLines are the seven v0.1 hook lines, including the
// post-commit hook v0.2 has no replacement for.
var legacyHookLines = map[string]string{
	"pre-commit":    "sanho hook pre-commit",
	"commit-msg":    `sanho hook commit-msg "$1"`,
	"pre-push":      `sanho hook pre-push "$@"`,
	"post-checkout": "sanho hook post-checkout",
	"post-merge":    "sanho hook post-merge",
	"post-rewrite":  `sanho hook post-rewrite "$@"`,
	"post-commit":   "sanho hook post-commit",
}

// seedV1Workspace turns a workspace into a v0.1 one: a config with
// socket_path and no schema_version, the legacy hash file, the seven
// v0.1 hook lines, and the daemon's state.json in SANHO_HOME.
//
// It is written by hand rather than produced by a v0.1 binary because
// the v0.1 client is deleted: the *file formats* are what the v0.2
// degradation path contracts with, so writing them out states the
// contract explicitly.
func seedV1Workspace(t *testing.T, ws *workspace, baseCommit string, withDaemonState bool) {
	t.Helper()

	writeFile(t, ws.path(".sanho.json"), `{
  "socket_path": "/tmp/sanhod.sock",
  "workspace_id": "`+projectName+`:`+ws.dir+`",
  "project": "`+projectName+`",
  "actor_email": "`+actorEmail+`",
  "docs_dir": "docs",
  "docs_hash_file": ".sanho_docs_hash"
}
`)
	writeFile(t, ws.path(".sanho_docs_hash"), baseCommit+"\n")

	for name, line := range legacyHookLines {
		writeExecutable(t, ws.hookPath(name), "#!/bin/sh\n"+line+"\n")
	}

	if withDaemonState {
		writeFile(t, filepath.Join(ws.w.home, "state.json"), `{
  "docs_repos": {
    "origin": {"ID": "origin", "Path": "`+ws.w.origin+`", "RepoURL": "`+ws.w.origin+`"}
  },
  "project_to_docs_repo": {"`+projectName+`": "origin"},
  "workspaces": {}
}
`)
	}
}
