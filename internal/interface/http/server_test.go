package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/state"
)

func TestHealthz(t *testing.T) {
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":5789"}, nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode healthz: %v", err)
	}
	if !body["ok"] {
		t.Fatalf("body = %#v, want ok=true", body)
	}
}

func TestRemovedAndUnknownEndpointsReturnJSON404(t *testing.T) {
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":5789"}, nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	paths := []string{
		"/",
		"/api",
		"/api/state",
		"/api/pty/sessions",
		"/api/pty/sessions/id/ws",
		"/docs",
		"/openapi.yaml",
		"/assets/app.js",
		"/unknown",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("request %s: %v", path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
			}
			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["error"] != "not_found" {
				t.Fatalf("error = %q, want not_found", body["error"])
			}
		})
	}
}

type mockGetStateUseCase struct {
	result *state.StateResult
	err    error
}

func (m *mockGetStateUseCase) Execute(context.Context) (*state.StateResult, error) {
	return m.result, m.err
}

func TestStateEndpointRemainsAvailable(t *testing.T) {
	mockUC := &mockGetStateUseCase{
		result: &state.StateResult{
			DocsHeads: map[docs.ProjectName]docs.CommitHash{"project1": "abc123"},
			Workspaces: []*workspace.Workspace{{
				ID:             "ws-1",
				Project:        "project1",
				DocsRepoID:     "repo-1",
				LocalPath:      "/tmp/ws1",
				RepoURL:        "https://github.com/test/repo",
				DocsHash:       "abc123",
				LastReportedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				LastActorEmail: "test@example.com",
			}},
		},
	}
	stateHandler := handler.NewStateHandler(mockUC)
	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":0"}, nil, nil, nil, nil, nil, stateHandler)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/state")
	if err != nil {
		t.Fatalf("request state: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		DocsHeads map[string]string `json:"docs_heads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if body.DocsHeads["project1"] != "abc123" {
		t.Fatalf("docs head = %q, want abc123", body.DocsHeads["project1"])
	}
}
