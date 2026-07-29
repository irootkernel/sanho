package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docs 존재 + docs-version 있음 + clean → init 성공 (reuse 모드)
func TestE2ECLI_InitReuseWithExistingDocs(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	originPath, head := createOriginRepo(t, map[string]string{
		"docs/index.md": "# reuse init\n",
	})

	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	setGitUser(t, wsDir, "reuse@example.com")
	runCmd(t, wsDir, "git", "remote", "add", "origin", originPath)

	// Seed docs content matching the docs repo HEAD and commit with docs-version tag.
	if err := os.MkdirAll(filepath.Join(wsDir, "docs", "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "docs", "docs", "index.md"), []byte("# reuse init\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	runCmd(t, wsDir, "git", "add", ".")
	commitMsg := fmt.Sprintf("seed docs\n\ndocs-version: %s", head)
	runCmd(t, wsDir, "git", "commit", "-m", commitMsg)

	project := "cli-init-reuse-" + strings.ReplaceAll(filepath.Base(wsDir), string(filepath.Separator), "_")
	t.Cleanup(func() {
		deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
	})

	cmd := exec.Command(cliBinary, "init",
		"--server-url", serverURL,
		"--project", project,
		"--docs-repo-url", originPath,
	)
	cmd.Dir = wsDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sanho init reuse failed: %v\nOutput:\n%s", err, string(out))
	}

	hashBytes, err := os.ReadFile(filepath.Join(wsDir, ".kkachi_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	if strings.TrimSpace(string(hashBytes)) != head {
		t.Fatalf("docs hash mismatch: got %s want %s", strings.TrimSpace(string(hashBytes)), head)
	}
	if !strings.Contains(string(out), "기존 docs 디렉토리를 그대로 사용하여 workspace 를 초기화했습니다.") {
		t.Fatalf("expected reuse mode message in output, got:\n%s", string(out))
	}

	assertGitignoreHasEntries(t, wsDir, "# Sanho", ".kkachi_docs_hash", ".kkachi.json")
}

// docs 존재 + docs-version 없음 → init 실패 (레거시 보호)
func TestE2ECLI_InitFailsOnLegacyDocsWithoutDocsVersion(t *testing.T) {
	cliBinary := getCliBinary(t)

	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	setGitUser(t, wsDir, "legacy@example.com")

	if err := os.MkdirAll(filepath.Join(wsDir, "docs", "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "docs", "docs", "index.md"), []byte("# legacy docs\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	runCmd(t, wsDir, "git", "add", ".")
	runCmd(t, wsDir, "git", "commit", "-m", "legacy docs without tag")

	cmd := exec.Command(cliBinary, "init",
		"--server-url", getServerURL(t),
		"--project", "legacy-no-tag",
		"--docs-repo-url", "git@example.com/some_docs.git",
	)
	cmd.Dir = wsDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected init to fail for legacy docs, but it succeeded:\n%s", string(out))
	}
	if !strings.Contains(string(out), "docs-version commits") {
		t.Fatalf("expected legacy protection message, got:\n%s", string(out))
	}
}

// docs 존재 + docs-version 있음 + dirty → init 실패
func TestE2ECLI_InitFailsWhenDocsDirty(t *testing.T) {
	cliBinary := getCliBinary(t)

	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	setGitUser(t, wsDir, "dirty@example.com")

	if err := os.MkdirAll(filepath.Join(wsDir, "docs", "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "docs", "docs", "index.md"), []byte("# dirty docs\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	runCmd(t, wsDir, "git", "add", ".")
	runCmd(t, wsDir, "git", "commit", "-m", "with tag\n\ndocs-version: deadbeef")

	// Make docs dirty (unstaged change)
	if err := os.WriteFile(filepath.Join(wsDir, "docs", "docs", "index.md"), []byte("# dirty docs\nchanged\n"), 0644); err != nil {
		t.Fatalf("modify docs: %v", err)
	}

	cmd := exec.Command(cliBinary, "init",
		"--server-url", getServerURL(t),
		"--project", "dirty-docs",
		"--docs-repo-url", "git@example.com/some_docs.git",
	)
	cmd.Dir = wsDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected init to fail for dirty docs, but it succeeded:\n%s", string(out))
	}
	if !strings.Contains(string(out), "commit 되지 않은 변경") {
		t.Fatalf("expected dirty docs message, got:\n%s", string(out))
	}
}
