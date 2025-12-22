package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/state"
)

func TestHealthz(t *testing.T) {
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":5789"}, nil, nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if !body["ok"] {
		t.Errorf("Expected ok: true, got %v", body)
	}
}

func TestAPIFallbackReturns404(t *testing.T) {
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":5789"}, nil, nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	testCases := []string{
		"/api",                  // exact /api (no trailing slash)
		"/api/",                 // /api/ (trailing slash)
		"/api/unknown",          // unknown subpath
		"/api/unknown/endpoint", // nested unknown path
	}

	for _, path := range testCases {
		t.Run("404 for "+path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("Expected status 404 for %s, got %d", path, resp.StatusCode)
			}

			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("Failed to decode response body: %v", err)
			}

			if body["error"] != "not_found" {
				t.Errorf("Expected error 'not_found' for %s, got '%s'", path, body["error"])
			}
		})
	}
}

// mockGetStateUseCase implements state.GetStateUseCase for testing
type mockGetStateUseCase struct {
	result *state.StateResult
	err    error
}

func (m *mockGetStateUseCase) Execute(ctx context.Context) (*state.StateResult, error) {
	return m.result, m.err
}

func TestAPIStateEndpoint(t *testing.T) {
	// Create mock use case with sample data
	mockUC := &mockGetStateUseCase{
		result: &state.StateResult{
			DocsHeads: map[docs.ProjectName]docs.CommitHash{
				"project1": "abc123",
			},
			Workspaces: []*workspace.Workspace{
				{
					ID:             "ws-1",
					Project:        "project1",
					DocsRepoID:     "repo-1",
					LocalPath:      "/tmp/ws1",
					RepoURL:        "https://github.com/test/repo",
					DocsHash:       "abc123",
					LastReportedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
					LastActorEmail: "test@example.com",
				},
			},
		},
	}

	stateHandler := handler.NewStateHandler(mockUC)
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":0"}, nil, nil, nil, nil, nil, stateHandler, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// Test that /api/state exists and returns valid response
	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("Failed to request /api/state: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for /api/state, got %d", resp.StatusCode)
	}

	var apiStateResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&apiStateResp); err != nil {
		t.Fatalf("Failed to decode /api/state response: %v", err)
	}

	// Test that /state returns the same response
	resp2, err := http.Get(ts.URL + "/state")
	if err != nil {
		t.Fatalf("Failed to request /state: %v", err)
	}
	defer resp2.Body.Close()

	var stateResp map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&stateResp); err != nil {
		t.Fatalf("Failed to decode /state response: %v", err)
	}

	// Compare responses (should be identical)
	docsHeads1, ok1 := apiStateResp["docs_heads"].(map[string]interface{})
	docsHeads2, ok2 := stateResp["docs_heads"].(map[string]interface{})
	if !ok1 || !ok2 {
		t.Error("Expected docs_heads in both responses")
	}

	if docsHeads1["project1"] != docsHeads2["project1"] {
		t.Errorf("/api/state and /state returned different docs_heads: %v vs %v", docsHeads1, docsHeads2)
	}
}

func TestStaticFileServing(t *testing.T) {
	// Create temp web dist directory
	tempDir, err := os.MkdirTemp("", "kkachi-web-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create index.html
	indexContent := `<!DOCTYPE html><html><body><h1>Kkachi Web</h1></body></html>`
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create assets dir
	assetsDir := filepath.Join(tempDir, "assets")
	if err := os.Mkdir(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create CSS file
	cssContent := `body { background: #f0f0f0; }`
	if err := os.WriteFile(filepath.Join(assetsDir, "style.css"), []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}

	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{
		Addr:       ":0",
		WebDistDir: tempDir,
	}, nil, nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	t.Run("Root serves index.html", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("Failed to request /: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != indexContent {
			t.Errorf("Expected index.html content, got %s", string(body))
		}
	})

	t.Run("Assets file served correctly", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/assets/style.css")
		if err != nil {
			t.Fatalf("Failed to request /assets/style.css: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /assets/style.css, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != cssContent {
			t.Errorf("Expected CSS content, got %s", string(body))
		}
	})

	t.Run("SPA fallback for /projects", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/projects")
		if err != nil {
			t.Fatalf("Failed to request /projects: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /projects, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != indexContent {
			t.Errorf("Expected index.html for /projects, got %s", string(body))
		}
	})

	t.Run("SPA fallback for /projects/:id", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/projects/sudal")
		if err != nil {
			t.Fatalf("Failed to request /projects/sudal: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /projects/sudal, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != indexContent {
			t.Errorf("Expected index.html for /projects/sudal, got %s", string(body))
		}
	})

	t.Run("SPA fallback for /debug/state", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/debug/state")
		if err != nil {
			t.Fatalf("Failed to request /debug/state: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /debug/state, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != indexContent {
			t.Errorf("Expected index.html for /debug/state, got %s", string(body))
		}
	})

	t.Run("Path traversal is blocked", func(t *testing.T) {
		// Attempt path traversal attack
		traversalPaths := []string{
			"/../../../etc/passwd",
			"/..%2F..%2F..%2Fetc%2Fpasswd",
			"/%2e%2e/%2e%2e/%2e%2e/etc/passwd",
		}

		for _, path := range traversalPaths {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("Failed to request %s: %v", path, err)
			}

			// Should either return SPA fallback (index.html) or 404, NOT file contents
			if resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				// If 200, it MUST be index.html (SPA fallback), not actual file content
				if string(body) != indexContent {
					t.Errorf("Path traversal might have succeeded for %s: got unexpected content", path)
				}
			}
			resp.Body.Close() // Close immediately, not defer in loop
			// 404 or any non-200 is also acceptable (attack blocked)
		}
	})
}

func TestWebDistNotFound(t *testing.T) {
	// Use a non-existent directory
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{
		Addr:       ":0",
		WebDistDir: "/nonexistent/web/dist",
	}, nil, nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	t.Run("Root returns error when web dist missing", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("Failed to request /: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 when web dist missing, got %d", resp.StatusCode)
		}

		var errResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode error response: %v", err)
		}

		if errResp["error"] != "web_dist_not_found" {
			t.Errorf("Expected error 'web_dist_not_found', got '%s'", errResp["error"])
		}
	})

	t.Run("SPA route returns error when web dist missing", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/projects/sudal")
		if err != nil {
			t.Fatalf("Failed to request /projects/sudal: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 for SPA route when web dist missing, got %d", resp.StatusCode)
		}
	})

	t.Run("API endpoints still work when web dist missing", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatalf("Failed to request /healthz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected /healthz to work even when web dist missing, got %d", resp.StatusCode)
		}
	})
}
