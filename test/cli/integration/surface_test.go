package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// The rest of the §5.8 command surface: the --json schemas agents read,
// registry administration, and the base re-derivation hooks.

func TestStatusJSONSchema(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")

	out := w.sanho(w.app, "status", "--refresh", "--json")

	var document struct {
		Project     string `json:"project"`
		WorkspaceID string `json:"workspace_id"`
		Base        *struct {
			Commit string `json:"commit"`
			Tree   string `json:"tree"`
		} `json:"base"`
		Canonical struct {
			Head              string `json:"head"`
			Tree              string `json:"tree"`
			Empty             bool   `json:"empty"`
			FetchedEver       bool   `json:"fetched_ever"`
			DataAgeSeconds    int64  `json:"data_age_seconds"`
			PublicationURL    string `json:"publication_url"`
			PublicationBranch string `json:"publication_branch"`
		} `json:"canonical"`
		Relation struct {
			Known  bool `json:"known"`
			Behind int  `json:"behind"`
			Ahead  int  `json:"ahead"`
		} `json:"relation"`
		SyncPreview struct {
			Known     bool     `json:"known"`
			Clean     bool     `json:"clean"`
			Conflicts []string `json:"conflicts"`
		} `json:"sync_preview"`
		SyncInProgress bool `json:"sync_in_progress"`
		Siblings       []struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"siblings"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &document); err != nil {
		t.Fatalf("parse status JSON: %v\n%s", err, out.stdout)
	}

	if document.Project != "product" {
		t.Errorf("project = %q, want product", document.Project)
	}
	if document.Base == nil || document.Base.Commit == "" {
		t.Errorf("base = %+v, want a recorded base", document.Base)
	}
	if document.Canonical.Head != w.canonicalHead() {
		t.Errorf("canonical head = %q, want %q", document.Canonical.Head, w.canonicalHead())
	}
	if document.Canonical.Empty || !document.Canonical.FetchedEver {
		t.Errorf("canonical = %+v, want a fetched, non-empty repository", document.Canonical)
	}
	if document.Canonical.PublicationBranch != "main" {
		t.Errorf("publication branch = %q, want main", document.Canonical.PublicationBranch)
	}
	if !document.Relation.Known || document.Relation.Behind != 1 {
		t.Errorf("relation = %+v, want known and behind 1", document.Relation)
	}
	if !document.SyncPreview.Known || !document.SyncPreview.Clean {
		t.Errorf("sync preview = %+v, want a known clean prediction", document.SyncPreview)
	}
	// A nil slice would render as null; agents get [].
	if document.SyncPreview.Conflicts == nil {
		t.Error("sync_preview.conflicts = null, want []")
	}
	if document.SyncInProgress {
		t.Error("sync_in_progress = true, want false")
	}
}

// Errors go to stderr; stdout under --json is either a document or
// nothing (§5.8).
func TestJSONErrorsNeverReachStdout(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	out := w.run(w.app, "status", "--json")
	if out.exitCode == 0 {
		t.Fatal("status outside a workspace succeeded, want a refusal")
	}
	if strings.TrimSpace(out.stdout) != "" {
		t.Fatalf("stdout = %q, want nothing on the JSON channel", out.stdout)
	}
	requireContains(t, "stderr", out.stderr, "not a sanho workspace")
}

func TestSyncJSONSchema(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	w.initAndAdoptDocs()

	// A clean sync.
	target := w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")

	clean := w.sanho(w.app, "sync", "--json")
	var synced struct {
		Status string `json:"status"`
		Base   *struct {
			Commit string `json:"commit"`
		} `json:"base"`
		Commit    string   `json:"commit"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal([]byte(clean.stdout), &synced); err != nil {
		t.Fatalf("parse sync JSON: %v\n%s", err, clean.stdout)
	}
	if synced.Status != "synced" || synced.Base == nil || synced.Base.Commit != target {
		t.Fatalf("sync JSON = %+v, want status synced at %s", synced, target)
	}
	if synced.Commit == "" {
		t.Error("commit = \"\", want the sync commit OID")
	}
	if synced.Conflicts == nil {
		t.Error("conflicts = null, want []")
	}

	// A conflicted sync is a success with status "conflicts", exit 0.
	w.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nTHEIRS\n",
		"guide.md": "upstream guide\n",
	}, "canonical: their edit")

	conflicted := w.run(w.app, "sync", "--json")
	if conflicted.exitCode != 0 {
		t.Fatalf("a conflicted sync exited %d, want 0 with status conflicts\n%s",
			conflicted.exitCode, conflicted.combined())
	}
	var withConflicts struct {
		Status    string   `json:"status"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal([]byte(conflicted.stdout), &withConflicts); err != nil {
		t.Fatalf("parse sync JSON: %v\n%s", err, conflicted.stdout)
	}
	if withConflicts.Status != "conflicts" {
		t.Fatalf("status = %q, want conflicts", withConflicts.Status)
	}
	if len(withConflicts.Conflicts) != 1 || withConflicts.Conflicts[0] != "docs/api.md" {
		t.Fatalf("conflicts = %v, want [docs/api.md]", withConflicts.Conflicts)
	}

	// And --abort reports its own status.
	aborted := w.sanho(w.app, "sync", "--abort", "--json")
	var abortDocument struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(aborted.stdout), &abortDocument); err != nil {
		t.Fatalf("parse abort JSON: %v\n%s", err, aborted.stdout)
	}
	if abortDocument.Status != "aborted" {
		t.Fatalf("status = %q, want aborted", abortDocument.Status)
	}
}

func TestStateJSONScopesToTheWorkspaceProjectAndAllWithFlag(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.sanho(w.app, "project", "add", "other", "--docs-repo-url", "https://example.test/other.git")

	type stateDocument struct {
		Home     string `json:"home"`
		Scope    string `json:"scope"`
		Projects []struct {
			Name        string `json:"name"`
			DocsRepoURL string `json:"docs_repo_url"`
			Head        string `json:"head"`
		} `json:"projects"`
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			LocalPath   string `json:"local_path"`
			BaseCommit  string `json:"base_commit"`
			ActorEmail  string `json:"actor_email"`
		} `json:"workspaces"`
	}

	var scoped stateDocument
	if err := json.Unmarshal([]byte(w.sanho(w.app, "state", "--json").stdout), &scoped); err != nil {
		t.Fatalf("parse state JSON: %v", err)
	}
	if scoped.Scope != "product" || len(scoped.Projects) != 1 {
		t.Fatalf("scoped state = %+v, want just the workspace's project", scoped)
	}
	// Heads come from the workspace's own clone (§5.2).
	if scoped.Projects[0].Head != w.canonicalHead() {
		t.Errorf("head = %q, want %q", scoped.Projects[0].Head, w.canonicalHead())
	}
	if len(scoped.Workspaces) != 1 || scoped.Workspaces[0].LocalPath != w.app {
		t.Fatalf("workspaces = %+v, want this one", scoped.Workspaces)
	}
	if scoped.Workspaces[0].BaseCommit == "" || scoped.Workspaces[0].ActorEmail == "" {
		t.Errorf("workspace row = %+v, want base and actor recorded", scoped.Workspaces[0])
	}

	var all stateDocument
	if err := json.Unmarshal([]byte(w.sanho(w.app, "state", "--all", "--json").stdout), &all); err != nil {
		t.Fatalf("parse state --all JSON: %v", err)
	}
	if all.Scope != "all" || len(all.Projects) != 2 {
		t.Fatalf("state --all = %+v, want both projects", all)
	}
}

func TestProjectAddGuardsURLConflictsAndDeleteGuardsWorkspaces(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	// The same URL is idempotent; a different one is refused, naming both
	// (audit M9's replacement).
	w.sanho(w.app, "project", "add", "product", "--docs-repo-url", w.origin)
	conflict := w.run(w.app, "project", "add", "product", "--docs-repo-url", "https://example.test/other.git")
	if conflict.exitCode != 1 {
		t.Fatalf("conflicting project add exited %d, want 1", conflict.exitCode)
	}
	requireContains(t, "conflict", conflict.stderr, w.origin)
	requireContains(t, "conflict", conflict.stderr, "https://example.test/other.git")

	// Delete refuses while a workspace still references the project.
	refused := w.run(w.app, "project", "delete", "product")
	if refused.exitCode != 1 {
		t.Fatalf("project delete exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "registered workspace")
	requireContains(t, "refusal", refused.stderr, "sanho clean")

	// --force is the deliberate override.
	w.sanho(w.app, "project", "delete", "product", "--force")
	requireNotContains(t, "state", w.sanho(w.app, "state", "--all", "--json").stdout, `"name": "product"`)
}

// §5.10: the base is a property of the checked-out content, so after
// HEAD moves it is recomputed from the trailers of the newly
// checked-out history — not carried across from the previous branch.
func TestBranchSwitchReDerivesTheBase(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	// `side` branches off the adopt commit, whose trailer names the base
	// canonical had at init time.
	first := w.canonicalHead()
	w.git(w.app, "branch", "side")

	// main then syncs to a newer canonical state, whose sync commit
	// carries the newer base.
	second := w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	w.sanho(w.app, "sync")
	requireContains(t, "base on main", readFile(t, w.appPath(".sanho_base.json")), second)

	// Switching to the older branch re-derives the older base, and says
	// so — the one line these hooks ever print.
	toSide := w.git(w.app, "checkout", "--quiet", "side")
	if got := readFile(t, w.appPath(".sanho_base.json")); !strings.Contains(got, first) {
		t.Fatalf("base on side = %s, want it re-derived to %s\nhook output:\n%s",
			got, first, toSide.combined())
	}
	requireContains(t, "post-checkout", toSide.combined(), "sanho: docs base re-derived as "+first[:12])

	// And switching back adopts the newer one again.
	toMain := w.git(w.app, "checkout", "--quiet", "main")
	if got := readFile(t, w.appPath(".sanho_base.json")); !strings.Contains(got, second) {
		t.Fatalf("base back on main = %s, want %s\nhook output:\n%s",
			got, second, toMain.combined())
	}
	requireContains(t, "post-checkout", toMain.combined(), "sanho: docs base re-derived as "+second[:12])
}

func TestDoctorJSONSchemaAndCleanRemoveDocs(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	out := w.sanho(w.app, "doctor", "--json")
	var document struct {
		Workspace string `json:"workspace"`
		Checks    []struct {
			Name     string `json:"name"`
			Severity string `json:"severity"`
			Detail   string `json:"detail"`
		} `json:"checks"`
		Warnings int `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &document); err != nil {
		t.Fatalf("parse doctor JSON: %v\n%s", err, out.stdout)
	}
	if document.Workspace == "" || len(document.Checks) == 0 {
		t.Fatalf("doctor JSON = %+v, want a workspace and checks", document)
	}
	if document.Warnings != 0 {
		t.Errorf("a freshly initialized workspace reported %d warnings:\n%s", document.Warnings, out.stdout)
	}
	names := map[string]bool{}
	for _, check := range document.Checks {
		names[check.Name] = true
	}
	for _, want := range []string{"git", "workspace-config", "hooks", "clone", "base", "registry", "sync", "docs"} {
		if !names[want] {
			t.Errorf("doctor reported no %q check", want)
		}
	}

	// --remove-docs takes the docs directory too.
	w.sanho(w.app, "clean", "-y", "--remove-docs")
	if fileExists(t, w.appPath("docs")) {
		t.Error("clean --remove-docs left the docs directory behind")
	}
}

// The two flags that only pass through to the use case: `pull --commit`
// records the update in history, and `sync --rebase-onto` accepts an
// explicit canonical target (and rejects one canonical does not have).
func TestPullCommitAndSyncRebaseOntoFlags(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	first := w.canonicalHead()
	second := w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")

	w.sanho(w.app, "pull", "--commit")
	subject := strings.TrimSpace(w.git(w.app, "log", "-1", "--format=%s").stdout)
	if subject != "docs: sync to "+second[:12] {
		t.Fatalf("pull --commit subject = %q, want 'docs: sync to %s'", subject, second[:12])
	}

	// --rebase-onto reconciles against an explicit canonical commit.
	rebased := w.sanho(w.app, "sync", "--rebase-onto", first, "--json")
	var document struct {
		Status string `json:"status"`
		Base   *struct {
			Commit string `json:"commit"`
		} `json:"base"`
	}
	if err := json.Unmarshal([]byte(rebased.stdout), &document); err != nil {
		t.Fatalf("parse sync JSON: %v\n%s", err, rebased.stdout)
	}
	if document.Base == nil || document.Base.Commit != first {
		t.Fatalf("sync --rebase-onto base = %+v, want %s", document.Base, first)
	}

	// A target canonical does not carry is refused rather than adopted.
	refused := w.run(w.app, "sync", "--rebase-onto", "0123456789abcdef0123456789abcdef01234567")
	if refused.exitCode != 1 {
		t.Fatalf("sync --rebase-onto <unknown> exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "not a canonical commit")
}
