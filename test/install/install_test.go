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

func TestGoInstallProducesBothBinaries(t *testing.T) {
	repoRoot := repositoryRoot(t)
	binDir := t.TempDir()

	install := exec.Command("go", "install", "./cmd/sanho", "./cmd/sanhod")
	install.Dir = repoRoot
	install.Env = append(os.Environ(), "GOBIN="+binDir)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("go install failed: %v\n%s", err, output)
	}

	tests := []struct {
		name       string
		args       []string
		wantPrefix string
	}{
		{name: "sanho", args: []string{"version"}, wantPrefix: "sanho version "},
		{name: "sanhod", args: []string{"--version"}, wantPrefix: "sanhod version "},
	}
	for _, test := range tests {
		binary := filepath.Join(binDir, test.name)
		info, err := os.Stat(binary)
		if err != nil {
			t.Fatalf("installed binary %s: %v", test.name, err)
		}
		if info.Mode()&0111 == 0 {
			t.Fatalf("installed binary %s is not executable", test.name)
		}
		output, err := exec.Command(binary, test.args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s version command failed: %v\n%s", test.name, err, output)
		}
		if !strings.HasPrefix(string(output), test.wantPrefix) {
			t.Fatalf("%s output = %q, want prefix %q", test.name, output, test.wantPrefix)
		}
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
