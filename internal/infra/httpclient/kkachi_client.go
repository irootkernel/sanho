// Package httpclient provides an HTTP client for communicating with sanhod.
//
// This package implements the KkachiClient interface for CLI operations such as
// workspace registration, docs head queries, and docs push operations.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

// Default configuration values.
const (
	DefaultTimeout = 30 * time.Second
)

// Error types for client operations.
var (
	// ErrUnknownProject is returned when the server responds with unknown_project.
	ErrUnknownProject = errors.New("project not registered on server")
	// ErrUnknownWorkspace is returned when the server responds with unknown_workspace.
	ErrUnknownWorkspace = errors.New("workspace not registered on server")
	// ErrWorkspaceProjectMismatch is returned when a workspace belongs to another project.
	ErrWorkspaceProjectMismatch = errors.New("workspace belongs to another project")
	// ErrUnknownDocsCommit is returned when the server responds with unknown_docs_commit.
	ErrUnknownDocsCommit = errors.New("docs commit not found in history")
	// ErrDocsRepoBusy is returned when the docs repo is being updated by another request.
	ErrDocsRepoBusy = errors.New("docs repo is busy")
	// ErrProjectHasWorkspaces is returned when trying to delete a project with workspaces.
	ErrProjectHasWorkspaces = errors.New("project has registered workspaces")
	// ErrEndpointNotFound is returned when the server does not expose an endpoint.
	ErrEndpointNotFound = errors.New("server endpoint not found")
	// ErrDocsHashNotInCurrentHistory is returned when a workspace reports a non-mainline docs hash.
	ErrDocsHashNotInCurrentHistory = errors.New("docs hash is not in current history")
	// ErrServerError is returned for non-specific server errors.
	ErrServerError = errors.New("server error")
)

// ---- Request/Response DTOs ----

// RegisterWorkspaceRequest is the request body for POST /workspaces/register.
type RegisterWorkspaceRequest struct {
	Project    docs.ProjectName `json:"project"`
	LocalPath  string           `json:"local_path"`
	RepoURL    string           `json:"repo_url"`
	ActorEmail string           `json:"actor_email"`
}

// RegisterWorkspaceResponse is the response from POST /workspaces/register.
type RegisterWorkspaceResponse struct {
	WorkspaceID     workspace.WorkspaceID `json:"workspace_id"`
	CurrentDocsHead docs.CommitHash       `json:"current_docs_head"`
}

type ReportWorkspaceDocsHashRequest struct {
	DocsHash   docs.CommitHash `json:"docs_hash"`
	ActorEmail string          `json:"actor_email"`
}

// DocsPushRequest is the request body for POST /docs/push.
type DocsPushRequest struct {
	WorkspaceID  workspace.WorkspaceID `json:"workspace_id"`
	BaseDocsHash docs.CommitHash       `json:"base_docs_hash"`
	DocsSnapshot string                `json:"docs_snapshot"` // base64-encoded tar.gz
	ActorEmail   string                `json:"actor_email"`
}

// DocsPushResponse is the response from POST /docs/push.
type DocsPushResponse struct {
	Ok              bool                `json:"ok"`
	Status          docs.DocsPushStatus `json:"status"`
	NewDocsHash     docs.CommitHash     `json:"new_docs_hash,omitempty"`
	CurrentDocsHash docs.CommitHash     `json:"current_docs_hash,omitempty"`
	Error           string              `json:"error,omitempty"`
}

// CreateProjectRequest is the request body for POST /projects.
type CreateProjectRequest struct {
	Project     docs.ProjectName `json:"project"`
	DocsRepoID  docs.DocsRepoID  `json:"docs_repo_id"`
	DocsRepoURL string           `json:"docs_repo_url"`
	ActorEmail  string           `json:"actor_email"`
}

// StateResponse is the response from GET /state.
// Matches server's ServerStateResponse structure.
type StateResponse struct {
	DocsHeads  map[string]string  `json:"docs_heads"`
	Workspaces []WorkspaceSummary `json:"workspaces"`
}

// WorkspaceSummary contains the summary of a registered workspace.
// Matches server's WorkspaceSummary structure.
type WorkspaceSummary struct {
	WorkspaceID    string  `json:"workspace_id"`
	Project        string  `json:"project"`
	DocsRepoID     string  `json:"docs_repo_id"`
	LocalPath      string  `json:"local_path"`
	RepoURL        string  `json:"repo_url"`
	DocsHash       string  `json:"docs_hash"`
	LastReportedAt *string `json:"last_reported_at,omitempty"`
	LastActorEmail string  `json:"last_actor_email"`
}

type CommitRelation struct {
	Status docs.CommitRelationStatus `json:"status"`
	Ahead  int                       `json:"ahead"`
	Behind int                       `json:"behind"`
}

type ProjectStatusWorkspace struct {
	WorkspaceID         string         `json:"workspace_id"`
	Project             string         `json:"project"`
	DocsRepoID          string         `json:"docs_repo_id"`
	LocalPath           string         `json:"local_path"`
	RepoURL             string         `json:"repo_url"`
	DocsHash            string         `json:"docs_hash"`
	LastReportedAt      *string        `json:"last_reported_at,omitempty"`
	LastActorEmail      string         `json:"last_actor_email"`
	RelativeToReference CommitRelation `json:"relative_to_reference"`
	RelativeToHead      CommitRelation `json:"relative_to_head"`
}

type ProjectStatusResponse struct {
	Project              string                   `json:"project"`
	ReferenceWorkspaceID string                   `json:"reference_workspace_id"`
	ReferenceDocsHash    string                   `json:"reference_docs_hash"`
	DocsHead             string                   `json:"docs_head"`
	ReferenceToHead      CommitRelation           `json:"reference_to_head"`
	Workspaces           []ProjectStatusWorkspace `json:"workspaces"`
}

// ---- Client Interface ----

// KkachiClient defines the interface for communicating with sanhod.
type KkachiClient interface {
	// DocsHead retrieves the current HEAD commit hash for the given project.
	DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error)

	// RegisterWorkspace registers a new workspace with the server.
	RegisterWorkspace(ctx context.Context, req RegisterWorkspaceRequest) (RegisterWorkspaceResponse, error)

	// ReportWorkspaceDocsHash records the docs commit currently adopted by a workspace.
	ReportWorkspaceDocsHash(ctx context.Context, workspaceID workspace.WorkspaceID, req ReportWorkspaceDocsHashRequest) error

	// DocsSnapshot retrieves a snapshot of docs at the specified commit (empty for HEAD).
	DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error)

	// DocsPush pushes a docs snapshot to the server.
	DocsPush(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error)

	// GetState retrieves the server state (optionally filtered by project).
	GetState(ctx context.Context, project *docs.ProjectName) (StateResponse, error)

	// GetProjectStatus compares all project workspaces to the caller's local docs hash.
	GetProjectStatus(
		ctx context.Context,
		project docs.ProjectName,
		workspaceID workspace.WorkspaceID,
		docsHash docs.CommitHash,
	) (ProjectStatusResponse, error)

	// CreateOrUpdateProject creates or updates a project on the server.
	CreateOrUpdateProject(ctx context.Context, req CreateProjectRequest) error

	// DeleteProject deletes a project from the server.
	DeleteProject(ctx context.Context, project docs.ProjectName, force bool) error

	// DeleteWorkspace deletes a workspace from the server.
	DeleteWorkspace(ctx context.Context, workspaceID workspace.WorkspaceID) error
}

// ---- HTTP Client Implementation ----

// HTTPClient implements KkachiClient using HTTP.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	maxRetries int
	retryDelay time.Duration
}

// HTTPClientOption is a functional option for configuring HTTPClient.
type HTTPClientOption func(*HTTPClient)

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) HTTPClientOption {
	return func(c *HTTPClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithMaxRetries sets the maximum retry count for busy errors.
func WithMaxRetries(retries int) HTTPClientOption {
	return func(c *HTTPClient) {
		c.maxRetries = retries
	}
}

// WithRetryDelay sets the delay between retries.
func WithRetryDelay(delay time.Duration) HTTPClientOption {
	return func(c *HTTPClient) {
		c.retryDelay = delay
	}
}

// NewHTTPClient creates a new HTTPClient.
func NewHTTPClient(baseURL string, opts ...HTTPClientOption) *HTTPClient {
	c := &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		maxRetries: 3,
		retryDelay: 300 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// DocsHead implements KkachiClient.DocsHead.
func (c *HTTPClient) DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	reqURL := fmt.Sprintf("%s/docs/head?project=%s", c.baseURL, url.QueryEscape(string(project)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkError(resp); err != nil {
		return "", err
	}

	var result struct {
		Head docs.CommitHash `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Head, nil
}

// RegisterWorkspace implements KkachiClient.RegisterWorkspace.
func (c *HTTPClient) RegisterWorkspace(ctx context.Context, req RegisterWorkspaceRequest) (RegisterWorkspaceResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return RegisterWorkspaceResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/workspaces/register", bytes.NewReader(body))
	if err != nil {
		return RegisterWorkspaceResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return RegisterWorkspaceResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkError(resp); err != nil {
		return RegisterWorkspaceResponse{}, err
	}

	var result RegisterWorkspaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RegisterWorkspaceResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

func (c *HTTPClient) ReportWorkspaceDocsHash(
	ctx context.Context,
	workspaceID workspace.WorkspaceID,
	req ReportWorkspaceDocsHashRequest,
) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	reqURL := fmt.Sprintf("%s/workspaces/%s/docs-hash", c.baseURL, url.PathEscape(string(workspaceID)))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	return c.checkError(resp)
}

// DocsSnapshot implements KkachiClient.DocsSnapshot.
func (c *HTTPClient) DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	reqURL := fmt.Sprintf("%s/docs/snapshot?project=%s", c.baseURL, url.QueryEscape(string(project)))
	if commit != "" {
		reqURL += "&commit=" + url.QueryEscape(string(commit))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkError(resp); err != nil {
		return nil, "", err
	}

	var result struct {
		Snapshot docs.DocsSnapshot `json:"snapshot"`
		Commit   docs.CommitHash   `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Snapshot, result.Commit, nil
}

// DocsPush implements KkachiClient.DocsPush with retry logic for busy errors.
func (c *HTTPClient) DocsPush(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error) {
	var lastErr error

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		resp, err := c.docsPushOnce(ctx, req)
		if err == nil {
			return resp, nil
		}

		if !errors.Is(err, ErrDocsRepoBusy) {
			return DocsPushResponse{}, err
		}

		lastErr = err
		if attempt < c.maxRetries-1 {
			select {
			case <-ctx.Done():
				return DocsPushResponse{}, ctx.Err()
			case <-time.After(c.retryDelay):
				// Retry
			}
		}
	}

	return DocsPushResponse{}, fmt.Errorf("%w: max retries exceeded", lastErr)
}

func (c *HTTPClient) docsPushOnce(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return DocsPushResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/docs/push", bytes.NewReader(body))
	if err != nil {
		return DocsPushResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return DocsPushResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkError(resp); err != nil {
		return DocsPushResponse{}, err
	}

	var result DocsPushResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return DocsPushResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetState implements KkachiClient.GetState.
func (c *HTTPClient) GetState(ctx context.Context, project *docs.ProjectName) (StateResponse, error) {
	reqURL := c.baseURL + "/state"
	if project != nil {
		reqURL += "?project=" + url.QueryEscape(string(*project))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return StateResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return StateResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkError(resp); err != nil {
		return StateResponse{}, err
	}

	var result StateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return StateResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

func (c *HTTPClient) GetProjectStatus(
	ctx context.Context,
	project docs.ProjectName,
	workspaceID workspace.WorkspaceID,
	docsHash docs.CommitHash,
) (ProjectStatusResponse, error) {
	query := url.Values{}
	query.Set("workspace_id", string(workspaceID))
	query.Set("docs_hash", string(docsHash))
	reqURL := fmt.Sprintf(
		"%s/projects/%s/status?%s",
		c.baseURL,
		url.PathEscape(string(project)),
		query.Encode(),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return ProjectStatusResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ProjectStatusResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkError(resp); err != nil {
		return ProjectStatusResponse{}, err
	}
	var result ProjectStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ProjectStatusResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// CreateOrUpdateProject implements KkachiClient.CreateOrUpdateProject.
func (c *HTTPClient) CreateOrUpdateProject(ctx context.Context, req CreateProjectRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/projects", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.checkError(resp)
}

// DeleteProject implements KkachiClient.DeleteProject.
func (c *HTTPClient) DeleteProject(ctx context.Context, project docs.ProjectName, force bool) error {
	reqURL := fmt.Sprintf("%s/projects/%s", c.baseURL, url.PathEscape(string(project)))
	if force {
		reqURL += "?force=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.checkError(resp)
}

// DeleteWorkspace implements KkachiClient.DeleteWorkspace.
func (c *HTTPClient) DeleteWorkspace(ctx context.Context, workspaceID workspace.WorkspaceID) error {
	reqURL := fmt.Sprintf("%s/workspaces/%s", c.baseURL, url.PathEscape(string(workspaceID)))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.checkError(resp)
}

// checkError parses the response and returns an appropriate error if not successful.
func (c *HTTPClient) checkError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		switch errResp.Error {
		case "unknown_project":
			return ErrUnknownProject
		case "unknown_workspace", "workspace_not_found":
			return ErrUnknownWorkspace
		case "workspace_project_mismatch":
			return ErrWorkspaceProjectMismatch
		case "unknown_docs_commit":
			return ErrUnknownDocsCommit
		case "docs_repo_busy":
			return ErrDocsRepoBusy
		case "project_has_workspaces":
			return ErrProjectHasWorkspaces
		case "not_found":
			return ErrEndpointNotFound
		case "docs_hash_not_in_current_history":
			return ErrDocsHashNotInCurrentHistory
		default:
			return fmt.Errorf("%w: %s (status %d)", ErrServerError, errResp.Error, resp.StatusCode)
		}
	}

	return fmt.Errorf("%w: status %d, body: %s", ErrServerError, resp.StatusCode, string(body))
}
