//go:build darwin || linux

package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v0.2 is daemonless (docs/architecture.md "Product boundary"): only the sanho CLI is
// installed. The v0.1 sanhod expectation this test carried is retired
// with the daemon itself (the package-boundary contract).
func TestGoInstallProducesTheCLIBinary(t *testing.T) {
	repoRoot := repositoryRoot(t)
	binDir := t.TempDir()

	install := exec.Command("go", "install", "./cmd/sanho")
	install.Dir = repoRoot
	install.Env = append(os.Environ(), "GOBIN="+binDir)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("go install failed: %v\n%s", err, output)
	}

	binary := filepath.Join(binDir, "sanho")
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("installed binary sanho: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatal("installed binary sanho is not executable")
	}
	output, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("sanho version command failed: %v\n%s", err, output)
	}
	line, ok := strings.CutSuffix(string(output), "\n")
	if !ok {
		t.Fatalf("sanho output = %q, want one trailing newline", output)
	}
	prefix, version, ok := strings.Cut(line, " ")
	if !ok || prefix != "sanho" || version == "" || strings.Contains(version, " ") {
		t.Fatalf("sanho output = %q, want %q", output, "sanho <version>\\n")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
