package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/project"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

// TestDocsPush_Integration tests the full /docs/push flow with real Git operations
func TestDocsPush_Integration(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// 1. Setup: Create temp directory structure
	tempDir, err := os.MkdirTemp("", "kkachi-push-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory so docs_repos is created in isolation
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	// 2. Create "remote" Git repo (origin)
	originPath := filepath.Join(tempDir, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, originPath, "init", "--bare")

	// 3. Clone to create local repo
	localPath := filepath.Join(tempDir, "local")
	runGitCmd(t, "", "clone", originPath, localPath)
	runGitCmd(t, localPath, "config", "user.email", "test@example.com")
	runGitCmd(t, localPath, "config", "user.name", "Test User")

	// Create initial docs
	docsDir := filepath.Join(localPath, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# Initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, localPath, "add", ".")
	runGitCmd(t, localPath, "commit", "-m", "Initial commit")
	runGitCmd(t, localPath, "push", "origin", "HEAD")

	// Get initial HEAD
	initialHead := strings.TrimSpace(string(runGitCmdOutput(t, localPath, "rev-parse", "HEAD")))

	// 4. Setup server
	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}

	gitClient := git.NewClient()
	mutexManager := git.NewRepoCoordinator()
	gitManager := git.NewDocsRepoManager(gitClient, mutexManager)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo, mutexManager)
	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)

	// Add project
	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	if err := addProjectUC.Execute(ctx, project.AddProjectInput{
		Project:     "test-project",
		DocsRepoID:  "test-repo",
		DocsRepoURL: originPath,
		ActorEmail:  "admin@test.com",
	}); err != nil {
		t.Fatalf("Failed to add project: %v", err)
	}

	// Register workspace
	registerWorkspaceUC := workspace.NewRegisterWorkspaceUseCase(docsRepo, workspaceRepo, stateRepo, workspace.RealClock{})
	ws, err := registerWorkspaceUC.Execute(ctx, workspace.RegisterWorkspaceCommand{
		Project:    "test-project",
		LocalPath:  "/tmp/test-ws",
		RepoURL:    originPath,
		ActorEmail: "dev@test.com",
	})
	if err != nil {
		t.Fatalf("Failed to register workspace: %v", err)
	}

	// 5. Create handlers
	deleteProjectUC := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	deleteWorkspaceUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	getDocsHeadUC := docs.NewGetDocsHeadUseCase(docsRepo)
	getDocsSnapshotUC := docs.NewGetDocsSnapshotUseCase(docsRepo)
	pushDocsUC := docs.NewPushDocsUseCase(workspaceRepo, docsRepo, mutexManager)

	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC, registerWorkspaceUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)
	docsSnapshotHandler := handler.NewDocsSnapshotHandler(getDocsSnapshotUC)
	docsPushHandler := handler.NewDocsPushHandler(pushDocsUC)

	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":0"}, projectHandler, workspaceHandler, docsHeadHandler, docsSnapshotHandler, docsPushHandler, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	client := ts.Client()

	// Test 1: Push with changes -> updated
	t.Run("Push with changes - Updated", func(t *testing.T) {
		snapshot := createSnapshot(t, map[string]string{
			"index.md": "# Updated Content\n",
			"new.md":   "# New File\n",
		})

		req := dto.DocsPushRequest{
			WorkspaceID:  string(ws.ID),
			BaseDocsHash: initialHead,
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev@test.com",
		}
		body, _ := json.Marshal(req)

		resp, err := client.Post(ts.URL+"/docs/push", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Push request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("Expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "updated" {
			t.Errorf("Expected status 'updated', got '%s'", pushResp.Status)
		}
		if pushResp.NewDocsHash == "" {
			t.Error("Expected NewDocsHash to be set")
		}
	})

	// Get new HEAD after first push from server's clone
	serverRepo, ok := stateRepo.GetDocsRepo("test-repo")
	if !ok {
		t.Fatal("Expected test-repo to be registered")
	}
	newHead := strings.TrimSpace(string(runGitCmdOutput(t, serverRepo.Path, "rev-parse", "HEAD")))

	// Test 2: Push identical content -> nochange
	t.Run("Push identical content - NoChange", func(t *testing.T) {
		// Re-register workspace to update docs_hash
		ws, _ = registerWorkspaceUC.Execute(ctx, workspace.RegisterWorkspaceCommand{
			Project:    "test-project",
			LocalPath:  "/tmp/test-ws",
			RepoURL:    originPath,
			ActorEmail: "dev@test.com",
		})

		snapshot := createSnapshot(t, map[string]string{
			"index.md": "# Updated Content\n",
			"new.md":   "# New File\n",
		})

		req := dto.DocsPushRequest{
			WorkspaceID:  string(ws.ID),
			BaseDocsHash: newHead,
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev@test.com",
		}
		body, _ := json.Marshal(req)

		resp, err := client.Post(ts.URL+"/docs/push", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Push request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("Expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "nochange" {
			t.Errorf("Expected status 'nochange', got '%s'", pushResp.Status)
		}
	})

	// Test 3: Push with outdated base -> outdated
	t.Run("Push with outdated base - Outdated", func(t *testing.T) {
		snapshot := createSnapshot(t, map[string]string{
			"index.md": "# Some Other Change\n",
		})

		req := dto.DocsPushRequest{
			WorkspaceID:  string(ws.ID),
			BaseDocsHash: initialHead, // Old base!
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev@test.com",
		}
		body, _ := json.Marshal(req)

		resp, err := client.Post(ts.URL+"/docs/push", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Push request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("Expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "outdated" {
			t.Errorf("Expected status 'outdated', got '%s'", pushResp.Status)
		}
		if pushResp.CurrentDocsHash == "" {
			t.Error("Expected CurrentDocsHash to be set")
		}
	})
}

func createSnapshot(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: "docs/" + name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("Failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write tar content: %v", err)
		}
	}

	tw.Close()
	gw.Close()

	return buf.Bytes()
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func runGitCmdOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return output
}
