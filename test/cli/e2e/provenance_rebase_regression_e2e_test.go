package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type provenanceRebaseFixture struct {
	cli         string
	socket      string
	project     string
	docsOrigin  string
	appOrigin   string
	workspace   string
	workspaceID string
	docsHash    string
}

func TestE2EOrphanedRebaseHeadCannotBypassCommitOrPushProvenance(t *testing.T) {
	fixture := setupProvenanceRebaseFixture(t)
	originalHead := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	rebaseHeadPath := gitOutput(t, fixture.workspace, "rev-parse", "--git-path", "REBASE_HEAD")
	if !filepath.IsAbs(rebaseHeadPath) {
		rebaseHeadPath = filepath.Join(fixture.workspace, rebaseHeadPath)
	}
	if err := os.WriteFile(rebaseHeadPath, []byte(originalHead+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	writeE2EFile(t, fixture.workspace, "docs/docs/index.md", "# orphaned edit\n")
	runCmd(t, fixture.workspace, "git", "add", "docs/docs/index.md")
	beforeBlockedCommit := captureE2EWorkspaceSafety(t, fixture, rebaseHeadPath)
	commit := exec.Command("git", "commit", "-m", "orphaned docs edit")
	commit.Dir = fixture.workspace
	commitOutput, commitErr := commit.CombinedOutput()
	if commitErr == nil {
		t.Fatalf("orphaned REBASE_HEAD allowed a normal docs commit:\n%s", commitOutput)
	}
	if after := captureE2EWorkspaceSafety(t, fixture, rebaseHeadPath); after != beforeBlockedCommit {
		t.Fatalf("blocked commit changed protected state:\nbefore=%+v\nafter=%+v", beforeBlockedCommit, after)
	}
	blockedPush := exec.Command("git", "push", "origin", "HEAD:refs/heads/orphan-probe")
	blockedPush.Dir = fixture.workspace
	blockedPushOutput, blockedPushErr := blockedPush.CombinedOutput()
	if blockedPushErr == nil || !strings.Contains(strings.ToLower(string(blockedPushOutput)), "orphaned rebase_head") {
		t.Fatalf("orphaned metadata did not block real push: err=%v\n%s", blockedPushErr, blockedPushOutput)
	}
	if refExists(t, fixture.appOrigin, "refs/heads/orphan-probe") {
		t.Fatal("orphaned metadata push created remote ref")
	}
	if after := captureE2EWorkspaceSafety(t, fixture, rebaseHeadPath); after != beforeBlockedCommit {
		t.Fatalf("blocked push changed protected state:\nbefore=%+v\nafter=%+v", beforeBlockedCommit, after)
	}

	runCmd(t, fixture.workspace, "git", "update-ref", "-d", "REBASE_HEAD", originalHead)
	emptyHooks := t.TempDir()
	bypass := exec.Command("git", "-c", "core.hooksPath="+emptyHooks, "commit", "-m", "unmanaged docs edit")
	bypass.Dir = fixture.workspace
	if output, err := bypass.CombinedOutput(); err != nil {
		t.Fatalf("create test-only unmanaged commit: %v\n%s", err, output)
	}
	unmanagedHead := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	runCmd(t, fixture.workspace, "git", "branch", "unmanaged-probe", unmanagedHead)
	runCmd(t, fixture.workspace, "git", "reset", "--hard", originalHead)

	unknownHash := strings.Repeat("f", 40)
	unknownCommit := exec.Command("git", "-c", "core.hooksPath="+emptyHooks, "commit", "--allow-empty", "-m", "unknown provenance", "-m", "docs-version: "+unknownHash)
	unknownCommit.Dir = fixture.workspace
	if output, err := unknownCommit.CombinedOutput(); err != nil {
		t.Fatalf("create test-only unknown provenance: %v\n%s", err, output)
	}
	runCmd(t, fixture.workspace, "git", "branch", "unknown-probe")
	writeE2EFile(t, fixture.workspace, "docs/docs/index.md", "# forged docs\n")
	runCmd(t, fixture.workspace, "git", "add", "docs/docs/index.md")
	forgedCommit := exec.Command("git", "-c", "core.hooksPath="+emptyHooks, "commit", "-m", "forged provenance", "-m", "docs-version: "+fixture.docsHash)
	forgedCommit.Dir = fixture.workspace
	if output, err := forgedCommit.CombinedOutput(); err != nil {
		t.Fatalf("create test-only forged provenance: %v\n%s", err, output)
	}
	runCmd(t, fixture.workspace, "git", "branch", "forged-probe")
	runCmd(t, fixture.workspace, "git", "reset", "--hard", originalHead)
	runCmd(t, fixture.workspace, "git", "branch", "valid-probe", originalHead)
	remoteBefore := bareRef(t, fixture.appOrigin, "refs/heads/main")
	push := exec.Command("git", "push", "origin", "unmanaged-probe", "unknown-probe", "forged-probe", "valid-probe")
	push.Dir = fixture.workspace
	pushOutput, pushErr := push.CombinedOutput()
	if pushErr == nil {
		t.Fatalf("pre-push accepted unmanaged docs tip %s:\n%s", unmanagedHead, pushOutput)
	}
	if got := bareRef(t, fixture.appOrigin, "refs/heads/main"); got != remoteBefore {
		t.Fatalf("rejected push changed remote main: %s -> %s", remoteBefore, got)
	}
	for _, ref := range []string{"unmanaged-probe", "unknown-probe", "forged-probe", "valid-probe"} {
		if refExists(t, fixture.appOrigin, "refs/heads/"+ref) {
			t.Fatalf("rejected multi-ref push partially created %s", ref)
		}
	}

	writeE2EFile(t, fixture.workspace, "docs/docs/index.md", "# managed edit\n")
	runCmd(t, fixture.workspace, "git", "add", "docs/docs/index.md")
	runCmd(t, fixture.workspace, "git", "commit", "-m", "managed docs edit")
	managedHead := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	managedDocsHash := docsVersionFromCommitMessage(t, fixture.workspace, managedHead)
	if managedDocsHash == "" || managedDocsHash == fixture.docsHash {
		t.Fatalf("normal hooks did not publish a new canonical docs version: %q", managedDocsHash)
	}
	runCmd(t, fixture.workspace, "git", "push", "origin", "main")
	if got := bareRef(t, fixture.appOrigin, "refs/heads/main"); got != managedHead {
		t.Fatalf("valid managed docs push remote main=%s want %s", got, managedHead)
	}

	legacyDocsHash := pushDocsViaHTTP(t, fixture.socket, fixture.workspaceID, managedDocsHash, map[string]string{
		"docs/index.md": "# legacy hook remote docs\n",
	}, "legacy-hook@example.com")
	writeE2EFile(t, fixture.workspace, "legacy-trigger.txt", "trigger pending main publication\n")
	runCmd(t, fixture.workspace, "git", "add", "legacy-trigger.txt")
	firstCommit := exec.Command("git", "commit", "-m", "trigger legacy hook publication")
	firstCommit.Dir = fixture.workspace
	firstCommitOutput, firstCommitErr := firstCommit.CombinedOutput()
	if firstCommitErr == nil || !strings.Contains(string(firstCommitOutput), "Run the same git commit command again") {
		t.Fatalf("remote docs did not prepare a pending main publication: err=%v\n%s", firstCommitErr, firstCommitOutput)
	}
	runCmd(t, fixture.workspace, "git", "commit", "-m", "trigger legacy hook publication")
	if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != legacyDocsHash {
		t.Fatalf("prepared workspace docs hash=%s want %s", got, legacyDocsHash)
	}
	governingMessage := gitOutput(t, fixture.workspace, "log", "-1", "--format=%B", "--grep=docs-version")
	if !strings.Contains(governingMessage, "docs-version: "+legacyDocsHash) {
		t.Fatalf("generated docs commit does not govern application HEAD:\n%s", governingMessage)
	}

	hooksDir := gitOutput(t, fixture.workspace, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(fixture.workspace, hooksDir)
	}
	prePushPath := filepath.Join(hooksDir, "pre-push")
	if err := os.WriteFile(prePushPath, []byte("#!/bin/sh\nsanho hook pre-push\n"), 0755); err != nil {
		t.Fatal(err)
	}
	legacyPush := exec.Command("git", "push", "origin", "main")
	legacyPush.Dir = fixture.workspace
	legacyOutput, legacyErr := legacyPush.CombinedOutput()
	legacyText := strings.ToLower(string(legacyOutput))
	if legacyErr == nil || !strings.Contains(legacyText, "retry the same git push") {
		t.Fatalf("minimal legacy hook did not request one retry: err=%v\n%s", legacyErr, legacyOutput)
	}
	for _, unexpected := range []string{"command not found", "origin: not found", "origin: command not found", fixture.appOrigin + ": not found"} {
		if strings.Contains(legacyText, strings.ToLower(unexpected)) {
			t.Fatalf("live legacy hook executed push arguments as shell commands (%q):\n%s", unexpected, legacyOutput)
		}
	}
	upgraded, err := os.ReadFile(prePushPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(upgraded), `sanho hook pre-push "$@"`) {
		t.Fatalf("legacy hook was not upgraded atomically:\n%s", upgraded)
	}
	runCmd(t, fixture.workspace, "git", "push", "origin", "main")
	writeE2EFile(t, fixture.workspace, "non-doc.txt", "valid descendant\n")
	runCmd(t, fixture.workspace, "git", "add", "non-doc.txt")
	runCmd(t, fixture.workspace, "git", "commit", "-m", "valid non-doc descendant")
	nonDocsHead := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	runCmd(t, fixture.workspace, "git", "push", "origin", "main")
	if got := bareRef(t, fixture.appOrigin, "refs/heads/main"); got != nonDocsHead {
		t.Fatalf("valid non-doc descendant push remote main=%s want %s", got, nonDocsHead)
	}

	runCmd(t, fixture.workspace, "git", "switch", "-c", "multi-one")
	writeE2EFile(t, fixture.workspace, "multi-one.txt", "one\n")
	runCmd(t, fixture.workspace, "git", "add", "multi-one.txt")
	runCmd(t, fixture.workspace, "git", "commit", "-m", "first valid multi-ref tip")
	multiOne := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	runCmd(t, fixture.workspace, "git", "branch", "multi-two", nonDocsHead)
	runCmd(t, fixture.workspace, "git", "push", "origin", "multi-one", "multi-two")
	if got := bareRef(t, fixture.appOrigin, "refs/heads/multi-one"); got != multiOne {
		t.Fatalf("valid multi-one remote=%s want %s", got, multiOne)
	}
	if got := bareRef(t, fixture.appOrigin, "refs/heads/multi-two"); got != nonDocsHead {
		t.Fatalf("valid multi-two remote=%s want %s", got, nonDocsHead)
	}

	linked := filepath.Join(sharedRepoTempDir(t), "linked")
	runCmd(t, fixture.workspace, "git", "worktree", "add", "-b", "linked-probe", linked, originalHead)
	configData, err := os.ReadFile(filepath.Join(fixture.workspace, ".sanho.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".sanho.json"), configData, 0644); err != nil {
		t.Fatal(err)
	}
	writeDocsHash(t, linked, fixture.docsHash)
	linkedMarker := gitOutput(t, linked, "rev-parse", "--git-path", "REBASE_HEAD")
	if !filepath.IsAbs(linkedMarker) {
		linkedMarker = filepath.Join(linked, linkedMarker)
	}
	if err := os.WriteFile(linkedMarker, []byte(originalHead+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeE2EFile(t, linked, "linked.txt", "linked\n")
	runCmd(t, linked, "git", "add", "linked.txt")
	linkedCommit := exec.Command("git", "commit", "-m", "linked orphan probe")
	linkedCommit.Dir = linked
	if output, err := linkedCommit.CombinedOutput(); err == nil {
		t.Fatalf("linked-worktree orphan metadata allowed commit:\n%s", output)
	}
}

func TestE2ERebaseLifecycleReconcilesAfterOperationClears(t *testing.T) {
	t.Run("fast-forward", func(t *testing.T) {
		fixture := setupProvenanceRebaseFixture(t)
		newDocsHash := pushDocsViaHTTP(t, fixture.socket, fixture.workspaceID, fixture.docsHash, map[string]string{
			"docs/index.md": "# remote docs\n",
		}, "remote@example.com")
		publishApplicationDocs(t, fixture, newDocsHash, "# remote docs\n", nil)

		beforeHash := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash"))
		output := runCmd(t, fixture.workspace, "git", "fetch", "origin")
		output = append(output, runCmd(t, fixture.workspace, "git", "rebase", "origin/main")...)
		if strings.Contains(string(output), "git rebase --abort") || strings.Contains(string(output), "git rebase --quit") {
			t.Fatalf("ordinary successful rebase printed recovery guidance:\n%s", output)
		}
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != beforeHash {
			t.Fatalf("active rebase hook mutated docs hash: %s -> %s", beforeHash, got)
		}
		assertNoRebaseMetadata(t, fixture.workspace)
		assertHeadReconciliation(t, fixture, newDocsHash, true, "up_to_date")

		writeE2EFile(t, fixture.workspace, "after-rebase.txt", "normal commit\n")
		runCmd(t, fixture.workspace, "git", "add", "after-rebase.txt")
		runCmd(t, fixture.workspace, "git", "commit", "-m", "normal commit after fast-forward rebase")
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != newDocsHash {
			t.Fatalf("pre-commit did not reconcile docs hash: got %s want %s", got, newDocsHash)
		}
		push := exec.Command("git", "push", "origin", "main")
		push.Dir = fixture.workspace
		if pushOutput, err := push.CombinedOutput(); err != nil {
			t.Fatalf("pre-push failed to reconcile valid HEAD: %v\n%s", err, pushOutput)
		}
	})

	t.Run("up-to-date-no-op", func(t *testing.T) {
		fixture := setupProvenanceRebaseFixture(t)
		before := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash"))
		output := runCmd(t, fixture.workspace, "git", "rebase", "origin/main")
		if strings.Contains(string(output), "git rebase --abort") || strings.Contains(string(output), "git rebase --quit") {
			t.Fatalf("no-op rebase printed recovery guidance:\n%s", output)
		}
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != before {
			t.Fatalf("no-op rebase changed docs hash: %s -> %s", before, got)
		}
		assertNoRebaseMetadata(t, fixture.workspace)
		assertHeadReconciliation(t, fixture, fixture.docsHash, false, "up_to_date")
	})

	t.Run("rewritten-commit", func(t *testing.T) {
		fixture, newDocsHash, oldHash, _ := setupDivergentProvenanceRebase(t, false)
		output := runCmd(t, fixture.workspace, "git", "fetch", "origin")
		output = append(output, runCmd(t, fixture.workspace, "git", "rebase", "origin/main")...)
		assertConciseLifecycleOutput(t, output)
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != oldHash {
			t.Fatalf("active rewrite mutated docs hash: %s -> %s", oldHash, got)
		}
		assertNoRebaseMetadata(t, fixture.workspace)
		assertHeadReconciliation(t, fixture, newDocsHash, true, "up_to_date")
		pushAndAssertReconciled(t, fixture, newDocsHash)
	})

	t.Run("conflict-continue", func(t *testing.T) {
		fixture, newDocsHash, oldHash, _ := setupDivergentProvenanceRebase(t, true)
		runCmd(t, fixture.workspace, "git", "fetch", "origin")
		rebase := exec.Command("git", "rebase", "origin/main")
		rebase.Dir = fixture.workspace
		output, err := rebase.CombinedOutput()
		if err == nil {
			t.Fatalf("expected rebase conflict:\n%s", output)
		}
		assertConciseLifecycleOutput(t, output)
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != oldHash {
			t.Fatalf("conflicted rebase mutated docs hash: %s -> %s", oldHash, got)
		}
		writeE2EFile(t, fixture.workspace, "app.txt", "resolved\n")
		runCmd(t, fixture.workspace, "git", "add", "app.txt")
		continueCmd := exec.Command("git", "rebase", "--continue")
		continueCmd.Dir = fixture.workspace
		continueCmd.Env = append(os.Environ(), "GIT_EDITOR=true")
		continueOutput, continueErr := continueCmd.CombinedOutput()
		if continueErr != nil {
			t.Fatalf("rebase --continue: %v\n%s", continueErr, continueOutput)
		}
		assertConciseLifecycleOutput(t, continueOutput)
		assertNoRebaseBackend(t, fixture.workspace)
		assertOrphanedRebaseMetadata(t, fixture)
		recoverOrphanedRebaseHeadIfPresent(t, fixture.workspace)
		assertNoRebaseMetadata(t, fixture.workspace)
		assertHeadReconciliation(t, fixture, newDocsHash, true, "up_to_date")
		pushAndAssertReconciled(t, fixture, newDocsHash)
	})

	t.Run("abort", func(t *testing.T) {
		fixture, _, oldHash, localHead := setupDivergentProvenanceRebase(t, true)
		runCmd(t, fixture.workspace, "git", "fetch", "origin")
		rebase := exec.Command("git", "rebase", "origin/main")
		rebase.Dir = fixture.workspace
		if output, err := rebase.CombinedOutput(); err == nil {
			t.Fatalf("expected rebase conflict:\n%s", output)
		}
		runCmd(t, fixture.workspace, "git", "rebase", "--abort")
		assertNoRebaseMetadata(t, fixture.workspace)
		if got := gitOutput(t, fixture.workspace, "rev-parse", "HEAD"); got != localHead {
			t.Fatalf("abort HEAD=%s want %s", got, localHead)
		}
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != oldHash {
			t.Fatalf("abort changed docs hash: %s -> %s", oldHash, got)
		}
		assertHeadReconciliation(t, fixture, oldHash, false, "outdated")
	})

	t.Run("quit-conflicted", func(t *testing.T) {
		fixture, _, oldHash, _ := setupDivergentProvenanceRebase(t, true)
		runCmd(t, fixture.workspace, "git", "fetch", "origin")
		rebase := exec.Command("git", "rebase", "origin/main")
		rebase.Dir = fixture.workspace
		if output, err := rebase.CombinedOutput(); err == nil {
			t.Fatalf("expected rebase conflict:\n%s", output)
		}
		runCmd(t, fixture.workspace, "git", "rebase", "--quit")
		assertNoRebaseBackend(t, fixture.workspace)
		if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != oldHash {
			t.Fatalf("quit changed docs hash: %s -> %s", oldHash, got)
		}
		push := exec.Command("git", "push", "origin", "HEAD:refs/heads/quit-probe")
		push.Dir = fixture.workspace
		output, err := push.CombinedOutput()
		lowerOutput := strings.ToLower(string(output))
		if err == nil || !strings.Contains(lowerOutput, "sanho hook pre-push") ||
			(!strings.Contains(lowerOutput, "unmerged") && !strings.Contains(lowerOutput, "orphaned rebase_head")) {
			t.Fatalf("quit-conflicted push was not blocked by remaining unsafe state: err=%v\n%s", err, output)
		}
		if refExists(t, fixture.appOrigin, "refs/heads/quit-probe") {
			t.Fatal("quit-conflicted push created remote ref")
		}
	})
}

type e2eWorkspaceSafety struct {
	Head             string
	IndexTree        string
	Status           string
	StagedDiff       string
	UnstagedDiff     string
	WorktreeFiles    string
	OperationMarker  string
	SanhoTransaction string
	LocalRefs        string
	RemoteRefs       string
}

func captureE2EWorkspaceSafety(
	t *testing.T,
	fixture provenanceRebaseFixture,
	markerPath string,
) e2eWorkspaceSafety {
	t.Helper()
	sanhoPath := gitOutput(t, fixture.workspace, "rev-parse", "--git-path", "sanho")
	if !filepath.IsAbs(sanhoPath) {
		sanhoPath = filepath.Join(fixture.workspace, sanhoPath)
	}
	return e2eWorkspaceSafety{
		Head:             gitOutput(t, fixture.workspace, "rev-parse", "HEAD"),
		IndexTree:        gitOutput(t, fixture.workspace, "write-tree"),
		Status:           string(runCmd(t, fixture.workspace, "git", "status", "--porcelain=v2", "-z", "--untracked-files=all")),
		StagedDiff:       string(runCmd(t, fixture.workspace, "git", "diff", "--cached", "--binary")),
		UnstagedDiff:     string(runCmd(t, fixture.workspace, "git", "diff", "--binary")),
		WorktreeFiles:    snapshotE2EPath(t, fixture.workspace, true),
		OperationMarker:  snapshotE2EPath(t, markerPath, false),
		SanhoTransaction: snapshotE2EPath(t, sanhoPath, false),
		LocalRefs:        string(runCmd(t, fixture.workspace, "git", "for-each-ref", "--format=%(refname) %(objectname)")),
		RemoteRefs:       string(runCmd(t, "", "git", "--git-dir", fixture.appOrigin, "for-each-ref", "--format=%(refname) %(objectname)")),
	}
}

func snapshotE2EPath(t *testing.T, root string, skipGit bool) string {
	t.Helper()
	entries := make([]string, 0)
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return "<absent>"
	} else if err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if skipGit && entry.IsDir() && path == filepath.Join(root, ".git") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content := ""
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content = fmt.Sprintf("%x", data)
		}
		entries = append(entries, fmt.Sprintf("%s %s %s", filepath.ToSlash(rel), info.Mode(), content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

func TestE2EPublishedInvalidBranchRecoveryWorkflow(t *testing.T) {
	fixture := setupProvenanceRebaseFixture(t)
	emptyHooks := t.TempDir()
	writeE2EFile(t, fixture.workspace, "docs/docs/index.md", "# already published invalid docs\n")
	runCmd(t, fixture.workspace, "git", "add", "docs/docs/index.md")
	bypassCommit := exec.Command("git", "-c", "core.hooksPath="+emptyHooks, "commit", "-m", "published invalid docs")
	bypassCommit.Dir = fixture.workspace
	if output, err := bypassCommit.CombinedOutput(); err != nil {
		t.Fatalf("create test-only invalid commit: %v\n%s", err, output)
	}
	invalidTip := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	bypassPush := exec.Command("git", "-c", "core.hooksPath="+emptyHooks, "push", "origin", "main")
	bypassPush.Dir = fixture.workspace
	if output, err := bypassPush.CombinedOutput(); err != nil {
		t.Fatalf("publish test-only invalid branch: %v\n%s", err, output)
	}
	if got := bareRef(t, fixture.appOrigin, "refs/heads/main"); got != invalidTip {
		t.Fatalf("invalid fixture remote=%s want %s", got, invalidTip)
	}

	writeE2EFile(t, fixture.workspace, "preserve-staged.txt", "staged\n")
	runCmd(t, fixture.workspace, "git", "add", "preserve-staged.txt")
	writeE2EFile(t, fixture.workspace, "app.txt", "unstaged\n")
	writeE2EFile(t, fixture.workspace, "preserve-untracked.txt", "untracked\n")
	runCmd(t, fixture.workspace, "git", "stash", "push", "--include-untracked", "-m", "sanho provenance repair: main")
	stashRef := gitOutput(t, fixture.workspace, "rev-parse", "refs/stash")

	runCmd(t, fixture.workspace, "git", "switch", "-c", "sanho-repair-main", invalidTip)
	runCmd(t, fixture.workspace, "git", "reset", "--soft", invalidTip+"^")
	repairCommit := exec.Command("git", "commit", "-C", invalidTip)
	repairCommit.Dir = fixture.workspace
	output, err := repairCommit.CombinedOutput()
	if err != nil && strings.Contains(string(output), "Run the same git commit command again") {
		repairCommit = exec.Command("git", "commit", "-C", invalidTip)
		repairCommit.Dir = fixture.workspace
		output, err = repairCommit.CombinedOutput()
	}
	if err != nil {
		t.Fatalf("repair commit through normal hooks: %v\n%s", err, output)
	}
	repairedTip := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	diff := exec.Command("git", "diff", "--exit-code", invalidTip, repairedTip)
	diff.Dir = fixture.workspace
	if diffOutput, diffErr := diff.CombinedOutput(); diffErr != nil {
		t.Fatalf("repaired tip tree differs from invalid tip %s: %v\n%s", invalidTip, diffErr, diffOutput)
	}
	if docsVersionFromCommitMessage(t, fixture.workspace, repairedTip) == "" {
		t.Fatal("repaired application commit has no docs-version trailer")
	}

	runCmd(t, fixture.workspace, "git", "branch", "-f", "main", repairedTip)
	runCmd(t, fixture.workspace, "git", "switch", "main")
	lease := "--force-with-lease=refs/heads/main:" + invalidTip
	push := exec.Command("git", "push", lease, "origin", "refs/heads/main:refs/heads/main")
	push.Dir = fixture.workspace
	if pushOutput, pushErr := push.CombinedOutput(); pushErr != nil {
		t.Fatalf("lease-protected repair push: %v\n%s", pushErr, pushOutput)
	}
	if got := bareRef(t, fixture.appOrigin, "refs/heads/main"); got != repairedTip {
		t.Fatalf("repaired remote=%s want %s", got, repairedTip)
	}

	runCmd(t, fixture.workspace, "git", "stash", "apply", "--index", stashRef)
	porcelain := gitOutput(t, fixture.workspace, "status", "--porcelain", "--untracked-files=all")
	for _, expected := range []string{"A  preserve-staged.txt", "M app.txt", "?? preserve-untracked.txt"} {
		if !strings.Contains(porcelain, expected) {
			t.Fatalf("preserved workspace state missing %q:\n%s", expected, porcelain)
		}
	}
	assertHeadReconciliation(t, fixture, docsVersionFromCommitMessage(t, fixture.workspace, repairedTip), false, "up_to_date")
}

func setupDivergentProvenanceRebase(t *testing.T, conflict bool) (provenanceRebaseFixture, string, string, string) {
	t.Helper()
	fixture := setupProvenanceRebaseFixture(t)
	oldHash := fixture.docsHash
	localPath := "local.txt"
	if conflict {
		localPath = "app.txt"
	}
	writeE2EFile(t, fixture.workspace, localPath, "local\n")
	runCmd(t, fixture.workspace, "git", "add", localPath)
	runCmd(t, fixture.workspace, "git", "commit", "-m", "local application change")
	localHead := gitOutput(t, fixture.workspace, "rev-parse", "HEAD")
	newDocsHash := pushDocsViaHTTP(t, fixture.socket, fixture.workspaceID, oldHash, map[string]string{
		"docs/index.md": "# remote docs\n",
	}, "remote@example.com")
	publishApplicationDocs(t, fixture, newDocsHash, "# remote docs\n", map[string]string{"app.txt": "remote\n"})
	return fixture, newDocsHash, oldHash, localHead
}

func assertConciseLifecycleOutput(t *testing.T, output []byte) {
	t.Helper()
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(strings.ToLower(line), "sanho") {
			continue
		}
		if strings.Contains(line, "git rebase --abort") || strings.Contains(line, "git rebase --quit") {
			t.Fatalf("Sanho lifecycle hook printed recovery guidance:\n%s", output)
		}
	}
}

func pushAndAssertReconciled(t *testing.T, fixture provenanceRebaseFixture, docsHash string) {
	t.Helper()
	push := exec.Command("git", "push", "origin", "main")
	push.Dir = fixture.workspace
	if output, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push valid rebased branch: %v\n%s", err, output)
	}
	if got := readTrimmedFile(t, filepath.Join(fixture.workspace, ".sanho_docs_hash")); got != docsHash {
		t.Fatalf("pre-push did not reconcile docs hash: got %s want %s", got, docsHash)
	}
}

func setupProvenanceRebaseFixture(t *testing.T) provenanceRebaseFixture {
	t.Helper()
	cli := getCliBinary(t)
	t.Setenv("PATH", filepath.Dir(cli)+string(os.PathListSeparator)+os.Getenv("PATH"))
	socket := getSocketPath(t)
	ensureDaemonAvailable(t, socket)
	docsOrigin, docsHash := createOriginRepo(t, map[string]string{"docs/index.md": "# base\n"})
	root := sharedRepoTempDir(t)
	workspace := filepath.Join(root, "workspace")
	appOrigin := filepath.Join(root, "app-origin.git")
	mustMkDir(t, workspace)
	initGitRepo(t, workspace)
	setGitUser(t, workspace, "provenance-rebase@example.com")
	writeE2EFile(t, workspace, ".gitignore", "# Sanho\n.sanho_docs_hash\n.sanho.json\n.sanho_workspace_report\n")
	writeE2EFile(t, workspace, "docs/docs/index.md", "# base\n")
	writeE2EFile(t, workspace, "app.txt", "base\n")
	runCmd(t, workspace, "git", "add", ".")
	runCmd(t, workspace, "git", "commit", "-m", "initial application", "-m", "docs-version: "+docsHash)
	runCmd(t, "", "git", "init", "--bare", "--initial-branch=main", appOrigin)
	runCmd(t, workspace, "git", "remote", "add", "origin", appOrigin)
	runCmd(t, workspace, "git", "push", "-u", "origin", "main")

	project := "provenance-rebase-" + filepath.Base(root)
	t.Cleanup(func() { deleteProjectViaCLI(t, cli, socket, project, true) })
	initCmd := exec.Command(cli, "init", "--socket", socket, "--project", project, "--docs-repo-url", docsOrigin)
	initCmd.Dir = workspace
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("sanho init: %v\n%s", err, output)
	}
	var config struct {
		WorkspaceID string `json:"workspace_id"`
	}
	data, err := os.ReadFile(filepath.Join(workspace, ".sanho.json"))
	if err != nil || json.Unmarshal(data, &config) != nil || config.WorkspaceID == "" {
		t.Fatalf("read initialized workspace config: %v", err)
	}
	return provenanceRebaseFixture{
		cli: cli, socket: socket, project: project, docsOrigin: docsOrigin,
		appOrigin: appOrigin, workspace: workspace, workspaceID: config.WorkspaceID, docsHash: docsHash,
	}
}

func publishApplicationDocs(t *testing.T, fixture provenanceRebaseFixture, docsHash, content string, extra map[string]string) string {
	t.Helper()
	publisher := filepath.Join(sharedRepoTempDir(t), "publisher")
	runCmd(t, "", "git", "clone", fixture.appOrigin, publisher)
	setGitUser(t, publisher, "publisher@example.com")
	writeE2EFile(t, publisher, "docs/docs/index.md", content)
	for path, value := range extra {
		writeE2EFile(t, publisher, path, value)
	}
	runCmd(t, publisher, "git", "add", ".")
	runCmd(t, publisher, "git", "commit", "-m", "publish application docs", "-m", "docs-version: "+docsHash)
	runCmd(t, publisher, "git", "push", "origin", "main")
	return gitOutput(t, publisher, "rev-parse", "HEAD")
}

func assertHeadReconciliation(t *testing.T, fixture provenanceRebaseFixture, docsHash string, pending bool, status string) {
	t.Helper()
	cmd := exec.Command(fixture.cli, "status", "--json")
	cmd.Dir = fixture.workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sanho status --json: %v\n%s", err, output)
	}
	var value map[string]any
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatalf("decode status: %v\n%s", err, output)
	}
	if value["status"] != status {
		t.Fatalf("status=%v want %s: %s", value["status"], status, output)
	}
	reconciliation, ok := value["head_reconciliation"].(map[string]any)
	if !ok || reconciliation["pending"] != pending || reconciliation["docs_hash"] != docsHash {
		t.Fatalf("head_reconciliation=%#v want pending=%v hash=%s", value["head_reconciliation"], pending, docsHash)
	}
}

func assertNoRebaseMetadata(t *testing.T, repo string) {
	t.Helper()
	for _, name := range []string{"rebase-merge", "rebase-apply", "REBASE_HEAD"} {
		path := gitOutput(t, repo, "rev-parse", "--git-path", name)
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo, path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rebase metadata %s remained: %v", path, err)
		}
	}
}

func assertNoRebaseBackend(t *testing.T, repo string) {
	t.Helper()
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		path := gitOutput(t, repo, "rev-parse", "--git-path", name)
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo, path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rebase backend %s remained: %v", path, err)
		}
	}
}

func assertOrphanedRebaseMetadata(t *testing.T, fixture provenanceRebaseFixture) {
	t.Helper()
	cmd := exec.Command(fixture.cli, "status", "--json")
	cmd.Dir = fixture.workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect completed rebase orphan metadata: %v\n%s", err, output)
	}
	var value struct {
		GitOperation struct {
			Active                 bool     `json:"active"`
			Orphaned               bool     `json:"orphaned"`
			Backend                string   `json:"backend"`
			RecoveryClassification string   `json:"recovery_classification"`
			NextCommands           []string `json:"next_commands"`
		} `json:"git_operation"`
	}
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatalf("decode orphan status: %v\n%s", err, output)
	}
	operation := value.GitOperation
	if !operation.Active || !operation.Orphaned || operation.Backend != "none" ||
		operation.RecoveryClassification != "conditional_pseudo_ref_delete" {
		t.Fatalf("completed conflict rebase operation=%+v", operation)
	}
	for _, command := range operation.NextCommands {
		if strings.Contains(command, "rebase --continue") || strings.Contains(command, "rebase --abort") || strings.Contains(command, "rebase --quit") {
			t.Fatalf("orphan recovery recommended unusable command %q", command)
		}
	}
}

func recoverOrphanedRebaseHeadIfPresent(t *testing.T, repo string) {
	t.Helper()
	marker := gitOutput(t, repo, "rev-parse", "--git-path", "REBASE_HEAD")
	if !filepath.IsAbs(marker) {
		marker = filepath.Join(repo, marker)
	}
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	oid := gitOutput(t, repo, "rev-parse", "--verify", "REBASE_HEAD")
	runCmd(t, repo, "git", "update-ref", "-d", "REBASE_HEAD", oid)
}

func bareRef(t *testing.T, repo, ref string) string {
	t.Helper()
	return strings.TrimSpace(string(runCmd(t, "", "git", "--git-dir", repo, "rev-parse", ref)))
}

func refExists(t *testing.T, repo, ref string) bool {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", repo, "show-ref", "--verify", "--quiet", ref)
	if err := cmd.Run(); err == nil {
		return true
	} else if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false
	} else {
		t.Fatalf("inspect ref %s: %v", ref, err)
		return false
	}
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(string(runCmd(t, repo, "git", args...)))
}

func readTrimmedFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func docsVersionFromCommitMessage(t *testing.T, repo, commit string) string {
	t.Helper()
	message := gitOutput(t, repo, "show", "-s", "--format=%B", commit)
	for _, line := range strings.Split(message, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "docs-version:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func writeE2EFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
