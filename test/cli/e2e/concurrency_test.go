package e2e

// Process-level concurrency (AGENTS.md testing rules).
//
// The unit suites run with `-race` and prove that the *code* is safe
// under goroutines. That is not the property v0.2 needs. Sanho is
// daemonless (D4): concurrency here means several short-lived `sanho`
// and `git` processes, in different checkouts, contending for one
// registry file and one canonical repository. Nothing in a single
// process's memory model says anything about that, so these tests spawn
// real processes and let the operating system schedule them.
//
//	C1  N processes hammering the registry flock (the state contract)
//	C2  two workspaces publishing into one canonical (the publication contract CAS retry)
//	C3  the guard for the one combination that is not concurrent by
//	    design: a push while a sync is unresolved

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// registryWriters is the N of "N processes hammering the registry".
// Eight is comfortably more than the flock's poll interval can serialize
// without waiting, so the 5-second acquisition budget is genuinely
// exercised rather than trivially met.
const registryWriters = 8

// publicationRaceIterations is how many times the two-workspace race is
// replayed. A race that only sometimes interleaves is a race that only
// sometimes tests anything, so it is run more than once.
const publicationRaceIterations = 5

// TestC1RegistryFlockSerializesConcurrentProcesses is the state contract's access
// protocol under contention: every writer either gets the lock inside
// its budget or says so with the lock path, and the file that results is
// the union of all of them — with its .bak sibling equally intact.
func TestC1RegistryFlockSerializesConcurrentProcesses(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	type attempt struct {
		project string
		res     result
		err     error
	}
	attempts := make([]attempt, registryWriters)

	// One barrier, N processes: they contend rather than queue politely.
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range attempts {
		attempts[i].project = fmt.Sprintf("project-%02d", i)
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			attempts[i].res, attempts[i].err = tryExecute(w.root, w.env(), cliBinary,
				"project", "add", attempts[i].project,
				"--docs-repo-url", fmt.Sprintf("https://example.test/%s.git", attempts[i].project))
		}(i)
	}
	close(start)
	group.Wait()

	// A writer may lose the lock race outright — that is a documented
	// outcome, provided it says so and provided retrying works.
	for i, a := range attempts {
		if a.err != nil {
			t.Fatalf("writer %d could not start: %v", i, a.err)
		}
		if a.res.exitCode == 0 {
			continue
		}
		requireContains(t, "lock timeout message", a.res.combined(), "state.lock")
		retry := execute(t, w.root, w.env(), cliBinary,
			"project", "add", a.project,
			"--docs-repo-url", fmt.Sprintf("https://example.test/%s.git", a.project))
		requireExit(t, "retry of "+a.project, retry, 0)
	}

	// Every registration landed, in both copies, and both parse.
	for _, name := range []string{"state.json", "state.json.bak"} {
		path := filepath.Join(w.home, name)
		var state struct {
			Version  int `json:"version"`
			Projects map[string]struct {
				DocsRepoURL string `json:"docs_repo_url"`
			} `json:"projects"`
			Workspaces map[string]any `json:"workspaces"`
		}
		raw := readFile(t, path)
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			t.Fatalf("%s is corrupt after %d concurrent writers: %v\n%s",
				name, registryWriters, err, raw)
		}
		if state.Version != 2 {
			t.Errorf("%s version = %d, want 2", name, state.Version)
		}
		for _, a := range attempts {
			project, ok := state.Projects[a.project]
			if !ok {
				t.Errorf("%s lost %s; %d concurrent writers must all be recorded",
					name, a.project, registryWriters)
				continue
			}
			if project.DocsRepoURL != "https://example.test/"+a.project+".git" {
				t.Errorf("%s has %s -> %q, want its own URL", name, a.project, project.DocsRepoURL)
			}
		}
	}
}

// TestC2TwoWorkspacesRacingToPublish is the publication contract CAS path driven by the
// operating system rather than by a stub: two real pushes, started
// together, into one canonical repository.
//
// Either interleaving is allowed. What is not allowed is a lost update —
// canonical must end up carrying both workspaces' docs, whichever push
// won — and the loser must, if it was rejected at all, have been told to
// sync and be able to.
func TestC2TwoWorkspacesRacingToPublish(t *testing.T) {
	t.Parallel()

	for iteration := 1; iteration <= publicationRaceIterations; iteration++ {
		t.Run(fmt.Sprintf("iteration-%d", iteration), func(t *testing.T) {
			t.Parallel()
			runPublicationRace(t)
		})
	}
}

func runPublicationRace(t *testing.T) {
	t.Helper()

	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	alpha := w.setupIsolated("alpha")
	bravo := w.setupIsolated("bravo")

	alpha.commitDocs("docs: alpha section", map[string]string{"alpha.md": "alpha\n"})
	bravo.commitDocs("docs: bravo section", map[string]string{"bravo.md": "bravo\n"})

	racers := []*workspace{alpha, bravo}
	results := make([]result, len(racers))
	errs := make([]error, len(racers))

	start := make(chan struct{})
	var group sync.WaitGroup
	for i, ws := range racers {
		group.Add(1)
		go func(i int, ws *workspace) {
			defer group.Done()
			<-start
			results[i], errs[i] = tryExecute(ws.dir, ws.env(), "git", "push", "--quiet", "origin", "main")
		}(i, ws)
	}
	close(start)
	group.Wait()

	won := 0
	for i, ws := range racers {
		if errs[i] != nil {
			t.Fatalf("%s could not start its push: %v", ws.name, errs[i])
		}
		if results[i].exitCode == 0 {
			won++
			continue
		}
		// Logged rather than asserted: which interleaving the kernel
		// produced is the one thing this test must not pin down. The log
		// is how a run reports whether it saw contention at all.
		t.Logf("%s was rejected and routed at sanho sync", ws.name)
		// A rejected racer must be routed at `sanho sync`, and that must
		// work — the CAS-exhausted and conflict rejections both say so
		// (the publication contract, the guidance contract), and D3 makes saying it a promise.
		requireContains(t, ws.name+" rejection", results[i].combined(), "sanho sync")
		requireExit(t, ws.name+" advised sync", ws.run("sync"), 0)
		requireExit(t, ws.name+" push after syncing", ws.push(), 0)
	}
	if won == 0 {
		t.Fatalf("neither push published anything:\nalpha:\n%s\nbravo:\n%s",
			results[0].combined(), results[1].combined())
	}
	t.Logf("publication race: %d of %d pushes published without being rejected", won, len(racers))

	// Convergence: no lost update, whichever order the kernel chose.
	head := w.canonicalHead()
	requireEqual(t, "canonical alpha.md", w.canonicalFile(head, "alpha.md"), "alpha\n")
	requireEqual(t, "canonical bravo.md", w.canonicalFile(head, "bravo.md"), "bravo\n")
	requireEqual(t, "canonical api.md", w.canonicalFile(head, "api.md"), "canonical api\n")

	// Canonical history stays linear: one commit per publish, no merges
	// (the publication contract canonical commit convention).
	for _, line := range strings.Split(strings.TrimSpace(
		w.git(w.origin, "log", "--format=%h %p", head).stdout), "\n") {
		if len(strings.Fields(line)) > 2 {
			t.Errorf("canonical history has a merge commit: %q", line)
		}
	}

	// And both checkouts can consume the reconciled result.
	for _, ws := range racers {
		requireExit(t, ws.name+" final sync", ws.run("sync"), 0)
		requireEqual(t, ws.name+" docs/alpha.md", ws.readDocs("alpha.md"), "alpha\n")
		requireEqual(t, ws.name+" docs/bravo.md", ws.readDocs("bravo.md"), "bravo\n")
		assertNoMachineLocalSiblings(t, ws)
	}
}

// TestC2SameLinePublicationRaceRequiresExplicitResolution is the conflict
// half of the machine-boundary race. One CAS writer wins; the other must not
// turn a same-line merge into a silent choice, and its documented recovery
// must publish successfully after an explicit resolution.
func TestC2SameLinePublicationRaceRequiresExplicitResolution(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "value: base\n"})
	alpha := w.setupIsolated("same-line-alpha")
	bravo := w.setupIsolated("same-line-bravo")
	alpha.commitDocs("docs: alpha value", map[string]string{"api.md": "value: alpha\n"})
	bravo.commitDocs("docs: bravo value", map[string]string{"api.md": "value: bravo\n"})

	racers := []*workspace{alpha, bravo}
	results := make([]result, len(racers))
	errs := make([]error, len(racers))
	start := make(chan struct{})
	var group sync.WaitGroup
	for i, ws := range racers {
		group.Add(1)
		go func(i int, ws *workspace) {
			defer group.Done()
			<-start
			results[i], errs[i] = tryExecute(ws.dir, ws.env(), "git", "push", "--quiet", "origin", "main")
		}(i, ws)
	}
	close(start)
	group.Wait()

	winner, loser := -1, -1
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("%s could not start its push: %v", racers[i].name, errs[i])
		}
		if results[i].exitCode == 0 {
			winner = i
		} else {
			loser = i
		}
	}
	if winner < 0 || loser < 0 {
		t.Fatalf("same-line race exits = [%d, %d], want one winner and one rejected loser\nalpha:\n%s\nbravo:\n%s",
			results[0].exitCode, results[1].exitCode, results[0].combined(), results[1].combined())
	}
	requireContains(t, "same-line loser", results[loser].combined(), "conflict with upstream")
	requireContains(t, "same-line loser", results[loser].combined(), "sanho sync")
	assertNoMachineLocalSiblings(t, racers[winner])
	assertNoMachineLocalSiblings(t, racers[loser])

	sync := racers[loser].sanho("sync")
	requireContains(t, "loser sync", sync.stdout, "have conflicts")
	racers[loser].writeDocs(map[string]string{"api.md": "value: resolved\n"})
	racers[loser].git("add", "docs/api.md")
	racers[loser].git("commit", "-m", "docs: resolve publication race")
	racers[loser].sanho("sync", "--continue")
	requireExit(t, "resolved loser push", racers[loser].push(), 0)
	requireEqual(t, "resolved canonical content", w.canonicalFile(w.canonicalHead(), "api.md"), "value: resolved\n")

	for _, line := range strings.Split(strings.TrimSpace(
		w.git(w.origin, "log", "--format=%h %p", w.canonicalHead()).stdout), "\n") {
		if len(strings.Fields(line)) > 2 {
			t.Errorf("canonical history has a merge commit: %q", line)
		}
	}
}

func assertNoMachineLocalSiblings(t *testing.T, ws *workspace) {
	t.Helper()
	var status struct {
		Siblings []struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"siblings"`
	}
	out := ws.sanho("status", "--json")
	if err := json.Unmarshal([]byte(out.stdout), &status); err != nil {
		t.Fatalf("parse %s status: %v\n%s", ws.name, err, out.stdout)
	}
	if len(status.Siblings) != 0 {
		t.Fatalf("%s machine-local siblings = %+v, want none", ws.name, status.Siblings)
	}
}

// TestC3PushDuringAnUnresolvedSyncIsRefused pins the one combination
// v0.2 does not support concurrently, and does not try to.
//
// A sync and a push in the *same* workspace are not two operations to
// interleave: an unresolved sync owns the docs worktree, so the publication contract step 2
// refuses the push outright rather than publishing half a resolution.
// The refusal's own closure — that `sanho sync --abort` works from
// there — is the push_sync_in_progress row of the closure suite; this
// test is about the guard holding and nothing reaching canonical.
func TestC3PushDuringAnUnresolvedSyncIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	ws := w.setup("guarded")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	published := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync", conflicted.stdout, "have conflicts")

	push := ws.push()
	requireExit(t, "push during an unresolved sync", push, 1)
	requireContains(t, "refusal", push.combined(), "sanho: finish the sync first")
	requireContains(t, "refusal", push.combined(), "sanho sync --abort")
	requireContains(t, "refusal", push.combined(), "error: push rejected")

	// The guard is a gate, not a warning: canonical is untouched.
	requireEqual(t, "canonical head", w.canonicalHead(), published)

	// Repeating the push repeats the refusal — no state was consumed.
	requireExit(t, "second push during the same sync", ws.push(), 1)
	requireEqual(t, "canonical head after the second push", w.canonicalHead(), published)
}
