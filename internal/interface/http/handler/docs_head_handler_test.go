package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
)

type mockGetDocsHeadUseCase struct {
	head docs.CommitHash
	err  error
}

func (m *mockGetDocsHeadUseCase) Execute(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	return m.head, m.err
}

func TestDocsHead(t *testing.T) {
	uc := &mockGetDocsHeadUseCase{head: "abcdef123456"}
	h := handler.NewDocsHeadHandler(uc)

	req := httptest.NewRequest(http.MethodGet, "/docs/head?project=test", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["head"] != "abcdef123456" {
		t.Errorf("Expected head abcdef123456, got %s", resp["head"])
	}
}

func TestDocsHead_UnknownProject(t *testing.T) {
	uc := &mockGetDocsHeadUseCase{err: docs.ErrUnknownProject}
	h := handler.NewDocsHeadHandler(uc)

	req := httptest.NewRequest(http.MethodGet, "/docs/head?project=unknown", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "unknown_project" {
		t.Errorf("Expected error unknown_project, got %s", resp["error"])
	}
}
