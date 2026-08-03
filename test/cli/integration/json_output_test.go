package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIVersionJSON(t *testing.T) {
	cmd := exec.Command(getCliBinary(t), "version", "--json")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("version --json failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output, err)
	}
	if len(got) != 2 || got["name"] != "sanho" || got["version"] == "" {
		t.Fatalf("version JSON = %#v", got)
	}
}

func TestCLIStateJSONFiltersCurrentProject(t *testing.T) {
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"docs_heads": map[string]string{
				"test-project": "test-head",
				"other":        "other-head",
			},
			"workspaces": []map[string]any{
				{
					"workspace_id":     "test-workspace-123",
					"project":          "test-project",
					"docs_hash":        "test-hash",
					"local_path":       "/private/test",
					"repo_url":         "git@example.com:test.git",
					"last_actor_email": "actor@example.com",
				},
				{
					"workspace_id": "other-workspace",
					"project":      "other",
					"docs_hash":    "other-hash",
				},
			},
		})
	}))
	defer daemon.Close()

	workspaceDir := t.TempDir()
	setupSanhoConfig(t, workspaceDir, daemon.SocketPath)
	cmd := exec.Command(getCliBinary(t), "state", "--json")
	cmd.Dir = workspaceDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("state --json failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output, err)
	}
	if got["scope"] != "project" || got["project"] != "test-project" {
		t.Fatalf("scope/project = %#v", got)
	}
	docsHeads := got["docs_heads"].(map[string]any)
	if len(docsHeads) != 1 || docsHeads["test-project"] != "test-head" {
		t.Fatalf("docs_heads = %#v", docsHeads)
	}
	workspaces := got["workspaces"].([]any)
	if len(workspaces) != 1 {
		t.Fatalf("workspaces = %#v", workspaces)
	}
	workspace := workspaces[0].(map[string]any)
	if _, ok := workspace["local_path"]; ok {
		t.Fatalf("state JSON exposed local_path: %#v", workspace)
	}
	if workspace["last_actor"] != "actor@example.com" {
		t.Fatalf("last_actor = %#v", workspace["last_actor"])
	}
}

func TestCLIStatusJSON(t *testing.T) {
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/test-project/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project":                "test-project",
			"reference_workspace_id": "test-workspace-123",
			"reference_docs_hash":    "initial-hash-123",
			"docs_head":              "head-hash-456",
			"reference_to_head": map[string]any{
				"status": "behind",
				"ahead":  0,
				"behind": 2,
			},
			"workspaces": []map[string]any{
				{
					"workspace_id": "peer-ahead",
					"repo_url":     "git@github.com:org/ahead.git",
					"local_path":   "/private/ahead",
					"docs_hash":    "full-ahead-hash",
					"relative_to_reference": map[string]any{
						"status": "ahead",
						"ahead":  2,
						"behind": 0,
					},
					"relative_to_head": map[string]any{
						"status": "same",
						"ahead":  0,
						"behind": 0,
					},
				},
			},
		})
	}))
	defer daemon.Close()

	workspaceDir := t.TempDir()
	setupSanhoConfig(t, workspaceDir, daemon.SocketPath)
	if output, err := exec.Command("git", "-C", workspaceDir, "init", "--initial-branch=main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	gitPathOutput, err := exec.Command(
		"git",
		"-C",
		workspaceDir,
		"rev-parse",
		"--git-path",
		"rebase-merge",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("resolve rebase path: %v\n%s", err, gitPathOutput)
	}
	rebasePath := strings.TrimSpace(string(gitPathOutput))
	if !filepath.IsAbs(rebasePath) {
		rebasePath = filepath.Join(workspaceDir, rebasePath)
	}
	if err := os.MkdirAll(rebasePath, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(getCliBinary(t), "status", "--json")
	cmd.Dir = workspaceDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output, err)
	}
	if got["status"] != "outdated" ||
		got["docs_base"] != "initial-hash-123" ||
		got["docs_head"] != "head-hash-456" {
		t.Fatalf("status JSON = %#v", got)
	}
	workspaces := got["workspaces"].([]any)
	workspace := workspaces[0].(map[string]any)
	if workspace["repository"] != "ahead" || workspace["docs_hash"] != "full-ahead-hash" {
		t.Fatalf("workspace JSON = %#v", workspace)
	}
	if _, ok := workspace["local_path"]; ok {
		t.Fatalf("status JSON exposed local_path: %#v", workspace)
	}
	gitOperation := got["git_operation"].(map[string]any)
	if gitOperation["active"] != true || gitOperation["type"] != "rebase" ||
		gitOperation["classification"] != "blocked" {
		t.Fatalf("git_operation = %#v", gitOperation)
	}
	commands := gitOperation["next_commands"].([]any)
	if len(commands) != 4 || commands[0] != "git status" || commands[3] != "git rebase --quit" {
		t.Fatalf("next_commands = %#v", commands)
	}
	second := exec.Command(getCliBinary(t), "status", "--json")
	second.Dir = workspaceDir
	secondOutput, err := second.Output()
	if err != nil {
		t.Fatalf("repeated status --json failed: %v", err)
	}
	if !bytes.Equal(output, secondOutput) {
		t.Fatalf("repeated status output changed:\nfirst=%s\nsecond=%s", output, secondOutput)
	}
	if info, err := os.Stat(rebasePath); err != nil || !info.IsDir() {
		t.Fatalf("status changed rebase metadata: info=%v err=%v", info, err)
	}
}

func TestCLIStatusJSONMarksLegacyComparisonUnavailable(t *testing.T) {
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/test-project/status":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
		case "/docs/head":
			_ = json.NewEncoder(w).Encode(map[string]string{"head": "initial-hash-123"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()

	workspaceDir := t.TempDir()
	setupSanhoConfig(t, workspaceDir, daemon.SocketPath)
	cmd := exec.Command(getCliBinary(t), "status", "--json")
	cmd.Dir = workspaceDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("status --json fallback failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output, err)
	}
	if got["workspace_comparisons_available"] != false {
		t.Fatalf("comparison availability = %#v", got["workspace_comparisons_available"])
	}
	if workspaces := got["workspaces"].([]any); len(workspaces) != 0 {
		t.Fatalf("workspaces = %#v, want empty", workspaces)
	}
}

func TestCLIStateAllJSONOutsideWorkspace(t *testing.T) {
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"docs_heads": map[string]string{"alpha": "head"},
			"workspaces": []map[string]any{},
		})
	}))
	defer daemon.Close()

	cmd := exec.Command(
		getCliBinary(t),
		"state",
		"--all",
		"--socket",
		daemon.SocketPath,
		"--json",
	)
	cmd.Dir = t.TempDir()
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("state --all --json failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output, err)
	}
	if got["scope"] != "all" || got["project"] != nil {
		t.Fatalf("scope/project = %#v", got)
	}
}

func TestCLIJSONErrorUsesStderrOnly(t *testing.T) {
	cmd := exec.Command(getCliBinary(t), "status", "--json")
	cmd.Dir = t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		t.Fatal("status --json outside a workspace must fail")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON error %q: %v", stderr.String(), err)
	}
	if got.Error.Code != "not_in_workspace" || got.Error.Message == "" {
		t.Fatalf("JSON error = %#v", got)
	}
}

func TestCLIStatusJSONUsesStableDaemonErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		daemonCode string
		wantCode   string
	}{
		{"unknown project", http.StatusBadRequest, "unknown_project", "unknown_project"},
		{"unknown workspace", http.StatusNotFound, "unknown_workspace", "unknown_workspace"},
		{"workspace mismatch", http.StatusBadRequest, "workspace_project_mismatch", "workspace_project_mismatch"},
		{"unknown commit", http.StatusBadRequest, "unknown_docs_commit", "unknown_docs_commit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.statusCode)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": test.daemonCode})
			}))
			defer daemon.Close()

			workspaceDir := t.TempDir()
			setupSanhoConfig(t, workspaceDir, daemon.SocketPath)
			cmd := exec.Command(getCliBinary(t), "status", "--json")
			cmd.Dir = workspaceDir
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err == nil {
				t.Fatal("status --json must fail")
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			var got struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
				t.Fatalf("invalid JSON error %q: %v", stderr.String(), err)
			}
			if got.Error.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", got.Error.Code, test.wantCode)
			}
		})
	}
}

func TestCLIJSONOperationalErrors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		setupDir func(t *testing.T, dir string)
		wantCode string
	}{
		{
			name:     "state all reports unavailable default socket outside workspace",
			args:     []string{"state", "--all", "--json"},
			setupDir: func(t *testing.T, dir string) {},
			wantCode: "daemon_request_failed",
		},
		{
			name: "status reports unavailable daemon",
			args: []string{"status", "--json"},
			setupDir: func(t *testing.T, dir string) {
				setupSanhoConfig(t, dir, "/tmp/sanhod-unavailable.sock")
			},
			wantCode: "daemon_request_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspaceDir := t.TempDir()
			test.setupDir(t, workspaceDir)
			cmd := exec.Command(getCliBinary(t), test.args...)
			cmd.Dir = workspaceDir
			cmd.Env = append(os.Environ(), "SANHO_HOME="+filepath.Join(workspaceDir, "runtime"))
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err == nil {
				t.Fatalf("%v must fail", test.args)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			var got struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
				t.Fatalf("invalid JSON error %q: %v", stderr.String(), err)
			}
			if got.Error.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", got.Error.Code, test.wantCode)
			}
		})
	}
}
