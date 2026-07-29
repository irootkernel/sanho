// Package client provides domain types for the kkachi CLI client.
package client

import (
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
)

// DocsStatus represents the synchronization status of local docs with the server.
type DocsStatus string

const (
	// DocsStatusUnknown indicates the status could not be determined (e.g., server unreachable).
	DocsStatusUnknown DocsStatus = "unknown"
	// DocsStatusUpToDate indicates local docs are in sync with the server HEAD.
	DocsStatusUpToDate DocsStatus = "up_to_date"
	// DocsStatusOutdated indicates local docs are behind the server HEAD.
	DocsStatusOutdated DocsStatus = "outdated"
)

// IsZero returns true if the status is the zero value (empty string).
func (s DocsStatus) IsZero() bool {
	return s == ""
}

// String returns the string representation of the status.
func (s DocsStatus) String() string {
	return string(s)
}

// IsValid returns true if the status is a known valid value.
func (s DocsStatus) IsValid() bool {
	switch s {
	case DocsStatusUnknown, DocsStatusUpToDate, DocsStatusOutdated:
		return true
	default:
		return false
	}
}

// WorkspaceConfig represents the local workspace configuration stored in .kkachi.json.
type WorkspaceConfig struct {
	// ServerURL is the base URL of the kkachi-server.
	ServerURL string `json:"server_url"`
	// WorkspaceID is the unique identifier for this workspace.
	WorkspaceID workspace.WorkspaceID `json:"workspace_id"`
	// Project is the project name this workspace belongs to.
	Project docs.ProjectName `json:"project"`
	// ActorEmail is the email address of the user performing operations.
	ActorEmail string `json:"actor_email"`
	// DocsDir is the local docs directory path (default: "docs").
	DocsDir string `json:"docs_dir"`
	// DocsHashFile is the path to the file storing the current docs hash (default: ".kkachi_docs_hash").
	DocsHashFile string `json:"docs_hash_file,omitempty"`
	// PendingFixFile is the path to the pending fix state file (default: ".kkachi_pending_fix").
	PendingFixFile string `json:"pending_fix_file,omitempty"`
	// DocsSyncCommitMessage is the subject used for system-generated docs base commits.
	DocsSyncCommitMessage string `json:"docs_sync_commit_message,omitempty"`
}

// DefaultDocsDir is the default docs directory name.
const DefaultDocsDir = "docs"

// DefaultDocsHashFile is the default docs hash file name.
const DefaultDocsHashFile = ".kkachi_docs_hash"

// DefaultPendingFixFile is the default pending fix file name.
const DefaultPendingFixFile = ".kkachi_pending_fix"

// DefaultDocsSyncCommitMessage is used for system-generated docs base commits.
const DefaultDocsSyncCommitMessage = "[KKACHI] Update docs"

// ApplyDefaults sets default values for optional fields.
func (c *WorkspaceConfig) ApplyDefaults() {
	if c.DocsDir == "" {
		c.DocsDir = DefaultDocsDir
	}
	if c.DocsHashFile == "" {
		c.DocsHashFile = DefaultDocsHashFile
	}
	if c.PendingFixFile == "" {
		c.PendingFixFile = DefaultPendingFixFile
	}
	if c.DocsSyncCommitMessage == "" {
		c.DocsSyncCommitMessage = DefaultDocsSyncCommitMessage
	}
}
