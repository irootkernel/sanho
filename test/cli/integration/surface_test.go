package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The rest of the JSON contract command surface: the --json schemas agents read,
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
		Publication struct {
			Known   bool `json:"known"`
			Pending bool `json:"pending"`
		} `json:"publication"`
		SyncPreview struct {
			Known     bool     `json:"known"`
			Clean     bool     `json:"clean"`
			Conflicts []string `json:"conflicts"`
		} `json:"sync_preview"`
		WorkingCopy struct {
			Known     bool `json:"known"`
			DocsClean bool `json:"docs_clean"`
		} `json:"working_copy"`
		LocalReadiness struct {
			Sync struct {
				Ready     bool     `json:"ready"`
				BlockedBy []string `json:"blocked_by"`
			} `json:"sync"`
			Pull struct {
				Ready     bool     `json:"ready"`
				BlockedBy []string `json:"blocked_by"`
			} `json:"pull"`
		} `json:"local_readiness"`
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
	if !document.Publication.Known || document.Publication.Pending {
		t.Errorf("publication = %+v, want known and not pending", document.Publication)
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
	if !document.WorkingCopy.Known || !document.WorkingCopy.DocsClean {
		t.Errorf("working_copy = %+v, want known and clean", document.WorkingCopy)
	}
	if !document.LocalReadiness.Sync.Ready || !document.LocalReadiness.Pull.Ready {
		t.Errorf("local_readiness = %+v, want sync and pull ready", document.LocalReadiness)
	}
	if document.LocalReadiness.Sync.BlockedBy == nil || document.LocalReadiness.Pull.BlockedBy == nil {
		t.Error("local_readiness blocked_by = null, want []")
	}
}

func TestStatusReportsDirtyDocsAsLocallyBlocked(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	writeFile(t, w.appPath("docs/api.md"), "working edit\n")

	out := w.sanho(w.app, "status", "--json")
	var document struct {
		WorkingCopy struct {
			Known     bool `json:"known"`
			DocsClean bool `json:"docs_clean"`
		} `json:"working_copy"`
		LocalReadiness struct {
			Sync struct {
				Ready     bool     `json:"ready"`
				BlockedBy []string `json:"blocked_by"`
			} `json:"sync"`
			Pull struct {
				Ready     bool     `json:"ready"`
				BlockedBy []string `json:"blocked_by"`
			} `json:"pull"`
		} `json:"local_readiness"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &document); err != nil {
		t.Fatalf("parse status JSON: %v\n%s", err, out.stdout)
	}
	if !document.WorkingCopy.Known || document.WorkingCopy.DocsClean {
		t.Errorf("working_copy = %+v, want known and dirty", document.WorkingCopy)
	}
	want := []string{"docs_dirty"}
	if document.LocalReadiness.Sync.Ready || !reflect.DeepEqual(document.LocalReadiness.Sync.BlockedBy, want) {
		t.Errorf("sync readiness = %+v, want blocked by docs_dirty", document.LocalReadiness.Sync)
	}
	if document.LocalReadiness.Pull.Ready || !reflect.DeepEqual(document.LocalReadiness.Pull.BlockedBy, want) {
		t.Errorf("pull readiness = %+v, want blocked by docs_dirty", document.LocalReadiness.Pull)
	}
}

func TestDiffInspectsIncomingDocsWithoutChangingTheWorkspace(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")

	beforeStatus := w.git(w.app, "status", "--porcelain=v1").stdout
	beforeBase := readFile(t, w.appPath(".sanho_base.json"))
	patch := w.sanho(w.app, "diff", "--refresh").stdout
	requireContains(t, "incoming diff path", patch, "b/guide.md")
	requireContains(t, "incoming diff content", patch, "+upstream guide")

	names := w.sanho(w.app, "diff", "--name-only").stdout
	if names != "guide.md\n" {
		t.Fatalf("diff --name-only = %q, want guide.md", names)
	}
	stat := w.sanho(w.app, "diff", "--stat").stdout
	requireContains(t, "incoming diffstat", stat, "guide.md")

	if after := w.git(w.app, "status", "--porcelain=v1").stdout; after != beforeStatus {
		t.Fatalf("git status changed after diff:\nbefore %q\nafter  %q", beforeStatus, after)
	}
	if after := readFile(t, w.appPath(".sanho_base.json")); after != beforeBase {
		t.Fatal("docs base changed after diff")
	}
}

func TestDiffLocalComparesBaseWithApplicationHead(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.commitDocs("docs: local update", map[string]string{"api.md": "local api\n"})

	patch := w.sanho(w.app, "diff", "--local").stdout
	requireContains(t, "local diff path", patch, "a/api.md")
	requireContains(t, "local diff content", patch, "+local api")
	if names := w.sanho(w.app, "diff", "--local", "--name-only").stdout; names != "api.md\n" {
		t.Fatalf("local diff --name-only = %q, want api.md", names)
	}

	for _, args := range [][]string{{"diff", "--stat", "--name-only"}, {"diff", "--local", "--refresh"}} {
		if result := w.run(w.app, args...); result.exitCode != 1 {
			t.Errorf("sanho %s exit = %d, want 1", strings.Join(args, " "), result.exitCode)
		}
	}
}

func TestDiffRefusesWithoutARecordedBase(t *testing.T) {
	w := newWorld(t, nil)
	w.initWorkspace()

	result := w.run(w.app, "diff")
	if result.exitCode != 1 {
		t.Fatalf("diff exit = %d, want 1", result.exitCode)
	}
	requireContains(t, "diff without base", result.stderr, "no docs base is recorded")
}

func TestInitReusesARegisteredProjectURL(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.sanho(w.app, "project", "add", "product", "--docs-repo-url", w.origin)

	result := w.sanho(w.app,
		"init",
		"--project", "product",
		"--actor-email", "author@example.test",
	)
	requireContains(t, "init summary", result.stdout, w.origin)
	w.git(w.app, "commit", "-m", "docs: adopt canonical docs")
	if status := w.sanho(w.app, "status", "--json"); !strings.Contains(status.stdout, `"publication_url": "`+w.origin+`"`) {
		t.Fatalf("status did not use registered URL:\n%s", status.stdout)
	}
}

func TestInitRequiresAURLForAnUnregisteredProject(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	result := w.run(w.app, "init", "--project", "unregistered")
	if result.exitCode != 1 {
		t.Fatalf("init exit = %d, want 1", result.exitCode)
	}
	requireContains(t, "init error", result.stderr, `--docs-repo-url is required because project "unregistered" is not registered`)
	if fileExists(t, w.appPath(".sanho.json")) {
		t.Fatal("init wrote config without a registered URL")
	}
}

func TestWorkspaceForgetRemovesOnlyAnExplicitStaleRow(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	statePath := filepath.Join(w.home, "state.json")

	var state map[string]any
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	workspaces := state["workspaces"].(map[string]any)
	staleID := "product:/missing/workspace"
	workspaces[staleID] = map[string]any{
		"project":         "product",
		"local_path":      filepath.Join(w.t.TempDir(), "deleted"),
		"base_commit":     "",
		"base_tree":       "",
		"actor_email":     "stale@example.test",
		"last_updated_at": "2026-08-13T00:00:00Z",
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("encode registry: %v", err)
	}
	writeFile(t, statePath, string(encoded)+"\n")

	result := w.sanho(w.app, "workspace", "forget", staleID)
	requireContains(t, "forget result", result.stdout, staleID)

	var after struct {
		Projects   map[string]json.RawMessage `json:"projects"`
		Workspaces map[string]json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &after); err != nil {
		t.Fatalf("parse registry after forget: %v", err)
	}
	if _, ok := after.Workspaces[staleID]; ok {
		t.Fatal("stale workspace row survived forget")
	}
	if _, ok := after.Workspaces["product:"+w.app]; !ok {
		t.Fatal("live workspace row was removed")
	}
	if _, ok := after.Projects["product"]; !ok {
		t.Fatal("project registration was removed")
	}
}

func TestWorkspaceForgetRefusesALiveCheckout(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	id := "product:" + w.app

	result := w.run(w.app, "workspace", "forget", id)
	if result.exitCode != 1 {
		t.Fatalf("workspace forget exit = %d, want 1", result.exitCode)
	}
	requireContains(t, "live workspace refusal", result.stderr, "still exists")
	state := w.sanho(w.app, "state", "--json").stdout
	requireContains(t, "live registry row", state, id)
}

func TestCheckEvaluatesExplicitPolicies(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	passed := w.sanho(w.app, "check", "--require-clean", "--require-current", "--require-published", "--json")
	var document struct {
		Passed bool `json:"passed"`
		Checks []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
			Reason string `json:"reason"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(passed.stdout), &document); err != nil {
		t.Fatalf("parse check JSON: %v\n%s", err, passed.stdout)
	}
	if !document.Passed || len(document.Checks) != 3 {
		t.Fatalf("check = %+v, want all three policies passing", document)
	}

	writeFile(t, w.appPath("docs/api.md"), "working edit\n")
	dirty := w.run(w.app, "check", "--require-clean", "--json")
	if dirty.exitCode != 1 {
		t.Fatalf("dirty check exit = %d, want 1", dirty.exitCode)
	}
	requireContains(t, "dirty policy result", dirty.stdout, `"reason": "docs_dirty"`)
	writeFile(t, w.appPath("docs/api.md"), "canonical api\n")

	w.advanceCanonical(map[string]string{"api.md": "upstream api\n"}, "canonical: advance")
	behind := w.run(w.app, "check", "--require-current", "--json")
	if behind.exitCode != 1 {
		t.Fatalf("current check exit = %d, want 1", behind.exitCode)
	}
	requireContains(t, "current policy result", behind.stdout, `"reason": "behind"`)

	w.commitDocs("docs: local update", map[string]string{"api.md": "local api\n"})
	pending := w.run(w.app, "check", "--require-published", "--json")
	if pending.exitCode != 1 {
		t.Fatalf("published check exit = %d, want 1", pending.exitCode)
	}
	requireContains(t, "published policy result", pending.stdout, `"reason": "publication_pending"`)
}

func TestCheckRequiresAnExplicitPolicyAndHandlesEmptyCanonical(t *testing.T) {
	w := newWorld(t, nil)
	w.initWorkspace()

	empty := w.sanho(w.app, "check", "--require-current", "--json")
	requireContains(t, "empty canonical policy", empty.stdout, `"reason": "canonical_empty"`)

	invalid := w.run(w.app, "check", "--json")
	if invalid.exitCode != 1 {
		t.Fatalf("policy-free check exit = %d, want 1", invalid.exitCode)
	}
	requireContains(t, "invalid argument envelope", invalid.stdout, `"code": "invalid_arguments"`)
}

func TestCheckOnlyRequiresCanonicalForCurrentPolicy(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	cloneDir := filepath.Join(w.app, ".git", "sanho", "canonical")
	if err := os.RemoveAll(cloneDir); err != nil {
		t.Fatalf("remove private canonical clone: %v", err)
	}

	clean := w.sanho(w.app, "check", "--require-clean", "--json")
	requireContains(t, "clean policy without clone", clean.stdout, `"passed": true`)

	published := w.sanho(w.app, "check", "--require-published", "--json")
	requireContains(t, "published policy without clone", published.stdout, `"reason": "published"`)

	writeFile(t, w.appPath("docs/api.md"), "working edit\n")
	dirty := w.run(w.app, "check", "--require-clean", "--json")
	if dirty.exitCode != 1 {
		t.Fatalf("dirty check without clone exit = %d, want 1", dirty.exitCode)
	}
	requireContains(t, "dirty policy without clone", dirty.stdout, `"reason": "docs_dirty"`)
	writeFile(t, w.appPath("docs/api.md"), "canonical api\n")

	w.commitDocs("docs: local update", map[string]string{"api.md": "local api\n"})
	pending := w.run(w.app, "check", "--require-published", "--json")
	if pending.exitCode != 1 {
		t.Fatalf("published check without clone exit = %d, want 1", pending.exitCode)
	}
	requireContains(t, "pending policy without clone", pending.stdout, `"reason": "publication_pending"`)

	current := w.run(w.app, "check", "--require-current", "--json")
	if current.exitCode != 1 {
		t.Fatalf("current check without clone exit = %d, want 1", current.exitCode)
	}
	requireContains(t, "current policy error envelope", current.stdout, `"code": "clone_missing"`)

	status := w.run(w.app, "status", "--json")
	if status.exitCode != 1 {
		t.Fatalf("status without clone exit = %d, want 1", status.exitCode)
	}
	requireContains(t, "status clone error envelope", status.stdout, `"code": "clone_missing"`)
}

func TestCheckCleanReportsGitEvaluationFailure(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	indexPath := w.appPath(".git", "index")
	if err := os.Remove(indexPath); err != nil {
		t.Fatalf("remove git index: %v", err)
	}
	if err := os.Mkdir(indexPath, 0o700); err != nil {
		t.Fatalf("replace git index with directory: %v", err)
	}

	out := w.run(w.app, "check", "--require-clean", "--json")
	if out.exitCode != 1 {
		t.Fatalf("clean evaluation failure exit = %d, want 1", out.exitCode)
	}
	requireContains(t, "clean evaluation error envelope", out.stdout, `"error": {`)
	requireNotContains(t, "clean evaluation policy result", out.stdout, `"checks":`)
}

func TestRegistryHidesAndPrunesSymlinkAliasesWithoutRewritingWorkspaceID(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	aliasRoot := filepath.Join(filepath.Dir(w.app), "app-alias")
	if err := os.Symlink(w.app, aliasRoot); err != nil {
		t.Fatalf("create workspace alias: %v", err)
	}
	canonicalKey := "product:" + w.app
	aliasKey := "product:" + aliasRoot

	configPath := w.appPath(".sanho.json")
	config := strings.Replace(readFile(t, configPath), canonicalKey, aliasKey, 1)
	writeFile(t, configPath, config)

	type workspaceRow struct {
		Project       string `json:"project"`
		LocalPath     string `json:"local_path"`
		BaseCommit    string `json:"base_commit"`
		BaseTree      string `json:"base_tree"`
		ActorEmail    string `json:"actor_email"`
		LastUpdatedAt string `json:"last_updated_at"`
	}
	var state struct {
		Version    int                        `json:"version"`
		Projects   map[string]json.RawMessage `json:"projects"`
		Workspaces map[string]workspaceRow    `json:"workspaces"`
	}
	statePath := filepath.Join(w.home, "state.json")
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	aliasRow := state.Workspaces[canonicalKey]
	aliasRow.LocalPath = aliasRoot
	state.Workspaces[aliasKey] = aliasRow
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("encode registry: %v", err)
	}
	writeFile(t, statePath, string(encoded)+"\n")

	var status struct {
		WorkspaceID string `json:"workspace_id"`
		Siblings    []struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"siblings"`
	}
	out := w.sanho(w.app, "status", "--json")
	if err := json.Unmarshal([]byte(out.stdout), &status); err != nil {
		t.Fatalf("parse status: %v\n%s", err, out.stdout)
	}
	if status.WorkspaceID != aliasKey {
		t.Fatalf("workspace_id = %q, want preserved %q", status.WorkspaceID, aliasKey)
	}
	if len(status.Siblings) != 0 {
		t.Fatalf("siblings = %+v, want filesystem aliases hidden", status.Siblings)
	}

	w.commitDocs("docs: publish through aliased identity", map[string]string{"api.md": "updated\n"})
	pushed := w.push()
	if pushed.exitCode != 0 {
		t.Fatalf("push failed with exit %d\n%s", pushed.exitCode, pushed.combined())
	}
	if got := readFile(t, configPath); got != config {
		t.Fatal("registry refresh rewrote the existing workspace_id")
	}
	state.Workspaces = nil
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatalf("parse refreshed registry: %v", err)
	}
	if _, ok := state.Workspaces[aliasKey]; ok {
		t.Fatalf("symlink alias survived registry refresh\npush:\n%s\nregistry:\n%s", pushed.combined(), readFile(t, statePath))
	}
	if _, ok := state.Workspaces[canonicalKey]; !ok {
		t.Fatal("canonical registry row is missing after refresh")
	}
}

// The prose goes to stderr; stdout under --json carries either the
// command's document or the JSON contract error envelope (F-M9). Both are JSON,
// so a machine reader never has to strip English off the channel it
// parses.
func TestJSONErrorsCarryAMachineEnvelope(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	out := w.run(w.app, "status", "--json")
	if out.exitCode == 0 {
		t.Fatal("status outside a workspace succeeded, want a refusal")
	}

	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, out.stdout)
	}
	if envelope.Error.Code != "not_in_workspace" {
		t.Fatalf("error code = %q, want not_in_workspace", envelope.Error.Code)
	}
	if envelope.Error.Message == "" {
		t.Error("the envelope carries no message")
	}
	// stdout is a JSON document and nothing else: no bare prose line
	// outside the envelope, which is what a parser would choke on.
	if !strings.HasPrefix(strings.TrimSpace(out.stdout), "{") {
		t.Errorf("stdout is not a bare JSON document: %s", out.stdout)
	}
	// The human channel is still stderr.
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
	// Heads come from the workspace's own clone (the private-clone contract).
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

// the hook contract: the base is a property of the checked-out content, so after
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
	if subject != "[SANHO] Sync docs to "+second[:12] {
		t.Fatalf("pull --commit subject = %q, want '[SANHO] Sync docs to %s'", subject, second[:12])
	}

	// --rebase-onto against an ancestor of a HEALTHY base is refused
	// (F-M4). The flag is rewrite recovery; adopting an older canonical
	// state as the base would make the next push "merge" documents nobody
	// reverted.
	backwards := w.run(w.app, "sync", "--rebase-onto", first)
	if backwards.exitCode != 1 {
		t.Fatalf("sync --rebase-onto <ancestor of a healthy base> exited %d, want 1", backwards.exitCode)
	}
	requireContains(t, "refusal", backwards.stderr, "--rebase-onto is for recovering from rewritten history")

	// A target canonical does not carry is refused rather than adopted.
	refused := w.run(w.app, "sync", "--rebase-onto", "0123456789abcdef0123456789abcdef01234567")
	if refused.exitCode != 1 {
		t.Fatalf("sync --rebase-onto <unknown> exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "not a canonical commit")

	// And it does reconcile against an explicit target once the recorded
	// base is genuinely unusable — canonical history replaced wholesale.
	rewritten := w.rewriteCanonical(map[string]string{"handbook.md": "new canonical\n"}, "canonical: rewritten")
	rebased := w.sanho(w.app, "sync", "--rebase-onto", rewritten, "--json")
	var document struct {
		Status string `json:"status"`
		Base   *struct {
			Commit string `json:"commit"`
		} `json:"base"`
	}
	if err := json.Unmarshal([]byte(rebased.stdout), &document); err != nil {
		t.Fatalf("parse sync JSON: %v\n%s", err, rebased.stdout)
	}
	if document.Base == nil || document.Base.Commit != rewritten {
		t.Fatalf("sync --rebase-onto base = %+v, want %s", document.Base, rewritten)
	}
}

func TestRewrittenHistoryRecoveryPublishesWithoutARestampCommit(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical v1\n"})
	w.initAndAdoptDocs()
	w.commitDocs("docs: publish v2", map[string]string{"api.md": "canonical v2\n"})
	if pushed := w.push(); pushed.exitCode != 0 {
		t.Fatalf("first publication failed\n%s", pushed.combined())
	}
	w.commitDocs("docs: publish shared guide", map[string]string{"shared.md": "shared\n"})
	if pushed := w.push(); pushed.exitCode != 0 {
		t.Fatalf("second publication failed\n%s", pushed.combined())
	}

	// This commit is based on the old canonical line. The rewrite then
	// removes every commit its provenance can name.
	w.commitDocs("docs: local work before rewrite", map[string]string{"local.md": "local\n"})
	rewritten := w.rewriteCanonical(map[string]string{
		"api.md":      "rewritten canonical\n",
		"upstream.md": "new upstream\n",
	}, "canonical: rewritten root")

	rejected := w.push()
	if rejected.exitCode == 0 {
		t.Fatal("push across rewritten history succeeded before reconciliation")
	}
	requireContains(t, "rewrite rejection", rejected.combined(), "canonical history was rewritten")

	merged := w.sanho(w.app, "sync", "--rebase-onto", rewritten)
	requireContains(t, "rewrite sync", merged.stdout, "have conflicts")
	writeFile(t, w.appPath("docs", "api.md"), "rewritten canonical\n")
	w.git(w.app, "add", "docs")
	w.git(w.app, "commit", "-m", "docs: resolve rewritten canonical")
	w.sanho(w.app, "sync", "--continue")
	requireContains(t, "status after recovery", w.sanho(w.app, "status").stdout, "sync      : up to date")
	requireContains(t, "sync after recovery", w.sanho(w.app, "sync").stdout, "up to date")

	// No dummy docs-changing commit is made after --continue. The
	// resolution tip's trailer still names the vanished pre-sync base;
	// its content absorption of the new head is the publication warrant.
	final := w.push()
	if final.exitCode != 0 {
		t.Fatalf("reconciled push failed without a dummy restamp commit\n%s", final.combined())
	}
	requireNotContains(t, "reconciled push", final.combined(), "uncorroborated_base")
	for path, want := range map[string]string{
		"api.md":      "rewritten canonical\n",
		"upstream.md": "new upstream\n",
		"local.md":    "local\n",
		"shared.md":   "shared\n",
	} {
		if got := w.canonicalFile(w.canonicalHead(), path); got != want {
			t.Errorf("canonical %s = %q, want %q", path, got, want)
		}
	}
}

// TestLogJSONSchema pins the reverse-traceability document: what
// publication has always recorded in canonical commits, now readable.
func TestLogJSONSchema(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.commitDocs("docs: local update", map[string]string{"api.md": "local api\n"})
	w.push()
	// A docs writer commits straight into the canonical repository.
	w.advanceCanonical(map[string]string{
		"api.md":   "local api\n",
		"guide.md": "upstream guide\n",
	}, "docs: hand-edited upstream")

	out := w.sanho(w.app, "log", "--refresh", "--json")

	var document struct {
		Branch         string `json:"branch"`
		FetchedEver    bool   `json:"fetched_ever"`
		DataAgeSeconds int64  `json:"data_age_seconds"`
		Entries        []struct {
			Commit      string `json:"commit"`
			Tree        string `json:"tree"`
			CommittedAt string `json:"committed_at"`
			Subject     string `json:"subject"`
			Kind        string `json:"kind"`
			Source      *struct {
				Repository        string `json:"repository"`
				Branch            string `json:"branch"`
				WorkspaceID       string `json:"workspace_id"`
				ApplicationCommit string `json:"application_commit"`
			} `json:"source"`
			ApplicationSubjects []string `json:"application_subjects"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &document); err != nil {
		t.Fatalf("decode log JSON: %v\n%s", err, out.stdout)
	}

	if document.Branch != "main" || !document.FetchedEver {
		t.Fatalf("canonical facts = %+v, want branch main and a recorded fetch", document)
	}
	// Newest first: the hand edit, this workspace's publication, and the
	// seed newWorld put in canonical before anything was published.
	if len(document.Entries) != 3 {
		t.Fatalf("log listed %d entries, want the hand edit, the publication and the seed", len(document.Entries))
	}
	if document.Entries[2].Kind != "external" {
		t.Errorf("seed commit kind = %q, want external", document.Entries[2].Kind)
	}

	external := document.Entries[0]
	if external.Kind != "external" {
		t.Errorf("upstream commit kind = %q, want external", external.Kind)
	}
	if external.Source != nil {
		t.Errorf("upstream commit source = %+v, want null", external.Source)
	}
	if external.ApplicationSubjects == nil {
		t.Error("upstream commit application_subjects is null, want []")
	}

	published := document.Entries[1]
	if published.Kind != "publication" {
		t.Fatalf("publication kind = %q, want publication", published.Kind)
	}
	if published.Source == nil {
		t.Fatal("publication reported no source")
	}
	if published.Source.Branch != "main" {
		t.Errorf("source branch = %q, want main", published.Source.Branch)
	}
	if published.Source.WorkspaceID == "" || published.Source.Repository == "" {
		t.Errorf("source = %+v, want a repository and workspace", published.Source)
	}
	// Machine OIDs are full length; the human form shortens them.
	tip := strings.TrimSpace(w.git(w.app, "rev-parse", "HEAD").stdout)
	if published.Source.ApplicationCommit != tip {
		t.Errorf("application_commit = %q, want the full-length pushed tip %q",
			published.Source.ApplicationCommit, tip)
	}
	if len(published.ApplicationSubjects) == 0 {
		t.Error("publication listed no application subjects")
	}
	if published.Commit == "" || published.Tree == "" || published.CommittedAt == "" {
		t.Errorf("publication row is incomplete: %+v", published)
	}
}

// TestLogSurvivesTheStatesItIsAdvisedIn covers the two states `sanho log`
// exists to serve: a canonical nothing has published into, and a
// workspace whose base is unusable. Neither may refuse.
func TestLogSurvivesTheStatesItIsAdvisedIn(t *testing.T) {
	w := newWorld(t, nil)
	w.initWorkspace()

	empty := w.sanho(w.app, "log", "--json")
	requireContains(t, "empty canonical entries", empty.stdout, `"entries": []`)

	w.commitDocs("docs: first", map[string]string{"api.md": "local api\n"})
	w.push()
	// Replace canonical history wholesale: the recorded base is gone and
	// no commit carries its tree. `sanho diff` refuses without a usable
	// base; log must not, because this is the state that names it.
	w.rewriteCanonical(map[string]string{"handbook.md": "an entirely new canonical\n"},
		"canonical: rewritten history")

	rewritten := w.sanho(w.app, "log", "--refresh")
	requireContains(t, "log after a rewrite", rewritten.stdout, "canonical: rewritten history")
}

func TestLogRejectsUnusableArgumentsWithAnEnvelope(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	for _, args := range [][]string{
		{"log", "--json", "-n", "0"},
		{"log", "--json", "--path", "../escape.md"},
		// An empty narrowing value is the shape an agent interpolating an
		// unset variable produces, and answering it with the whole
		// listing would attribute every entry to a source never named.
		{"log", "--json", "--repository", ""},
		{"log", "--json", "--workspace", ""},
		{"log", "--json", "--path", ""},
	} {
		out := w.run(w.app, args...)
		if out.exitCode != 1 {
			t.Fatalf("%v exit = %d, want 1", args, out.exitCode)
		}
		requireContains(t, "log argument envelope", out.stdout, `"code": "invalid_arguments"`)
	}
}

// TestLogFiltersBySource is the question the multi-repository model
// makes people ask: which of these documents came from which repository.
func TestLogFiltersBySource(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.commitDocs("docs: local update", map[string]string{"api.md": "local api\n"})
	w.push()
	// A docs writer commits straight into the canonical repository, and
	// quotes the publication subject while doing it.
	w.advanceCanonical(map[string]string{
		"api.md":   "local api\n",
		"guide.md": "see [SANHO] Publish docs from " + filepath.Base(w.app) + "/main for context\n",
	}, "docs: mention [SANHO] Publish docs from "+filepath.Base(w.app)+"/main")

	all := w.sanho(w.app, "log", "--refresh", "--json")
	var everything logDocument
	if err := json.Unmarshal([]byte(all.stdout), &everything); err != nil {
		t.Fatalf("decode log JSON: %v\n%s", err, all.stdout)
	}
	if len(everything.Entries) != 3 {
		t.Fatalf("unfiltered log listed %d entries, want 3", len(everything.Entries))
	}
	var publication struct{ repository, workspace string }
	for _, entry := range everything.Entries {
		if entry.Source != nil {
			publication.repository = entry.Source.Repository
			publication.workspace = entry.Source.WorkspaceID
		}
	}
	if publication.repository == "" || publication.workspace == "" {
		t.Fatal("no publication in the listing to filter on")
	}

	for name, args := range map[string][]string{
		"by repository": {"log", "--json", "--repository", publication.repository},
		"by workspace":  {"log", "--json", "--workspace", publication.workspace},
		"by both":       {"log", "--json", "--repository", publication.repository, "--workspace", publication.workspace},
	} {
		t.Run(name, func(t *testing.T) {
			out := w.sanho(w.app, args...)
			var filtered logDocument
			if err := json.Unmarshal([]byte(out.stdout), &filtered); err != nil {
				t.Fatalf("decode filtered log JSON: %v\n%s", err, out.stdout)
			}
			if len(filtered.Entries) != 1 {
				t.Fatalf("%v listed %d entries, want only the publication: %s", args, len(filtered.Entries), out.stdout)
			}
			entry := filtered.Entries[0]
			if entry.Kind != "publication" || entry.Source == nil {
				t.Fatalf("filtered entry = %+v, want a decoded publication", entry)
			}
			if entry.Source.Repository != publication.repository {
				t.Errorf("repository = %q, want %q", entry.Source.Repository, publication.repository)
			}
		})
	}

	// A source that published nothing here matches nothing, and the
	// human line says which narrowing came up empty rather than claiming
	// canonical is empty.
	none := w.sanho(w.app, "log", "--repository", "no-such-repository")
	requireContains(t, "empty filtered listing", none.stdout, "no canonical commits match repository no-such-repository")
	requireNotContains(t, "empty filtered listing", none.stdout, "canonical has no commits yet")
}

// logDocument mirrors the `log` schema in docs/cli-json.md.
type logDocument struct {
	Branch  string `json:"branch"`
	Entries []struct {
		Commit string `json:"commit"`
		Kind   string `json:"kind"`
		Source *struct {
			Repository  string `json:"repository"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"source"`
	} `json:"entries"`
}

// TestMalformedInvocationWritesAnEnvelope is the same contract one
// stage earlier: an argument cobra rejects never reaches the command, so
// nothing inside it could render the envelope. These fail before a
// workspace is resolved, which is why no init is needed.
func TestMalformedInvocationWritesAnEnvelope(t *testing.T) {
	w := newWorld(t, nil)

	for _, args := range [][]string{
		{"status", "--json", "extra"},
		{"state", "--json", "--no-such-flag"},
		{"check", "--require-clean", "--json", "extra"},
		// The flag's own value is what failed to parse. Reporting that
		// on an empty stdout would deny the envelope to the very flag
		// the invocation was engaging.
		{"status", "--json=maybe"},
	} {
		out := w.run(w.app, args...)
		if out.exitCode != 1 {
			t.Fatalf("%v exit = %d, want 1", args, out.exitCode)
		}
		requireContains(t, "malformed invocation envelope", out.stdout, `"code": "invalid_arguments"`)
		requireContains(t, "malformed invocation guidance", out.stderr, "sanho: invalid arguments: ")
	}
}

// TestUnknownSubcommandIsRefused pins the group refusal at the process
// boundary. `sanho hook` is the one git itself invokes, so its failure
// mode is worth proving against the real binary.
func TestUnknownSubcommandIsRefused(t *testing.T) {
	w := newWorld(t, nil)

	out := w.run(w.app, "hook", "no-such-hook")
	if out.exitCode != 1 {
		t.Fatalf("hook no-such-hook exit = %d, want 1", out.exitCode)
	}
	if out.stdout != "" {
		t.Fatalf("hook no-such-hook stdout = %q, want nothing", out.stdout)
	}
	requireContains(t, "unknown subcommand", out.stderr, `unknown command "no-such-hook" for "sanho hook"`)
}

func TestLogReportsAMissingClone(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	cloneDir := filepath.Join(w.app, ".git", "sanho", "canonical")
	if err := os.RemoveAll(cloneDir); err != nil {
		t.Fatalf("remove private canonical clone: %v", err)
	}

	out := w.run(w.app, "log", "--json")
	if out.exitCode != 1 {
		t.Fatalf("log without clone exit = %d, want 1", out.exitCode)
	}
	requireContains(t, "log clone error envelope", out.stdout, `"code": "clone_missing"`)
}

func TestShowJSONSchema(t *testing.T) {
	w := newWorld(t, map[string]string{
		"api.md":          "canonical api\n",
		"guides/setup.md": "setup guide\n",
	})
	w.initAndAdoptDocs()
	head := w.canonicalHead()

	listing := w.sanho(w.app, "show", head, "--json")
	var document showDocument
	if err := json.Unmarshal([]byte(listing.stdout), &document); err != nil {
		t.Fatalf("decode show JSON: %v\n%s", err, listing.stdout)
	}
	if document.Commit != head || document.Tree == "" {
		t.Fatalf("show reported commit/tree = %q/%q, want the full %q and a tree",
			document.Commit, document.Tree, head)
	}
	// Listing mode fills neither of the document fields, and an agent
	// must be able to read that rather than infer it.
	if document.Path != nil || document.Document != nil {
		t.Errorf("listing mode reported path=%v document=%+v, want both null",
			document.Path, document.Document)
	}
	paths := make([]string, 0, len(document.Entries))
	for _, entry := range document.Entries {
		paths = append(paths, entry.Path)
		if len(entry.OID) != 40 || entry.Mode == "" || entry.Size <= 0 {
			t.Errorf("entry %+v is incomplete", entry)
		}
	}
	if !reflect.DeepEqual(paths, []string{"api.md", "guides/setup.md"}) {
		t.Errorf("show listed %v, want the docs-root-relative paths", paths)
	}

	// An abbreviated OID resolves, because `sync --rebase-onto` accepts
	// one and inspecting a candidate must not need a different spelling.
	body := w.sanho(w.app, "show", head[:8], "--path", "guides/setup.md", "--json")
	var read showDocument
	if err := json.Unmarshal([]byte(body.stdout), &read); err != nil {
		t.Fatalf("decode show --path JSON: %v\n%s", err, body.stdout)
	}
	if read.Path == nil || *read.Path != "guides/setup.md" {
		t.Fatalf("path = %v, want guides/setup.md", read.Path)
	}
	if read.Document == nil || read.Document.Binary {
		t.Fatalf("document = %+v, want a text document", read.Document)
	}
	if read.Document.Content == nil || *read.Document.Content != "setup guide\n" {
		t.Errorf("content = %v, want the document's bytes", read.Document.Content)
	}
	if len(read.Entries) != 0 {
		t.Errorf("entries = %v, want [] in document mode", read.Entries)
	}

	// Human mode prints the document itself, so the output can be
	// redirected into a file.
	plain := w.sanho(w.app, "show", head, "--path", "api.md")
	if plain.stdout != "canonical api\n" {
		t.Errorf("show --path stdout = %q, want exactly the document", plain.stdout)
	}
}

// TestShowReadsWhatRewriteRecoveryNeeds is the state the command was
// added for: canonical history was replaced, so no base resolves and the
// candidate carries no provenance to decode. Only its content can settle
// whether it is the right anchor.
func TestShowReadsWhatRewriteRecoveryNeeds(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.commitDocs("docs: local update", map[string]string{"api.md": "local api\n"})
	w.push()

	rewritten := w.rewriteCanonical(map[string]string{
		"handbook.md": "an entirely new canonical\n",
	}, "canonical: rewritten history")
	w.sanho(w.app, "log", "--refresh")

	// `sanho diff` refuses here: it needs a usable base, and this is the
	// state that has none. Show must not.
	listing := w.sanho(w.app, "show", rewritten)
	requireContains(t, "rewrite candidate listing", listing.stdout, "handbook.md")

	body := w.sanho(w.app, "show", rewritten, "--path", "handbook.md")
	if body.stdout != "an entirely new canonical\n" {
		t.Errorf("candidate content = %q, want the rewritten document", body.stdout)
	}
}

func TestShowRejectsUnusableTargetsWithAnEnvelope(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	head := w.canonicalHead()

	tests := map[string]struct {
		args []string
		code string
	}{
		"unknown commit":   {args: []string{"show", strings.Repeat("b", 40), "--json"}, code: "unknown_target"},
		"unknown document": {args: []string{"show", head, "--path", "nope.md", "--json"}, code: "unknown_target"},
		"escaping path":    {args: []string{"show", head, "--path", "../escape.md", "--json"}, code: "invalid_arguments"},
		"no revision":      {args: []string{"show", "--json"}, code: "invalid_arguments"},
		"two revisions":    {args: []string{"show", head, head, "--json"}, code: "invalid_arguments"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			out := w.run(w.app, test.args...)
			if out.exitCode != 1 {
				t.Fatalf("%v exit = %d, want 1", test.args, out.exitCode)
			}
			requireContains(t, "show error envelope", out.stdout, `"code": "`+test.code+`"`)
		})
	}
}

// showDocument mirrors the schema docs/cli-json.md publishes, so a
// change to either shape has to be made in both places.
type showDocument struct {
	Commit  string  `json:"commit"`
	Tree    string  `json:"tree"`
	Path    *string `json:"path"`
	Entries []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		OID  string `json:"oid"`
		Size int64  `json:"size"`
	} `json:"entries"`
	Document *struct {
		OID     string  `json:"oid"`
		Size    int64   `json:"size"`
		Binary  bool    `json:"binary"`
		Content *string `json:"content"`
	} `json:"document"`
}
