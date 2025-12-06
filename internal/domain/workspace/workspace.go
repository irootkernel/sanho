package workspace

import (
	"context"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

type WorkspaceID string

type Workspace struct {
	ID             WorkspaceID
	Project        docs.ProjectName
	DocsRepoID     docs.DocsRepoID
	LocalPath      string
	RepoURL        string
	DocsHash       docs.CommitHash
	LastReportedAt time.Time
	OwnerEmail     string
	LastActorEmail string
}

type WorkspaceRepository interface {
	Save(ctx context.Context, ws *Workspace) error
	Get(ctx context.Context, id WorkspaceID) (*Workspace, error)
	UpdateDocsHash(ctx context.Context, id WorkspaceID, newHash docs.CommitHash, actorEmail string) error
	List(ctx context.Context) ([]*Workspace, error)
}
