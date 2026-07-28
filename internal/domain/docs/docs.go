package docs

import "context"

type ProjectName string
type CommitHash string

func (h CommitHash) IsZero() bool {
	return h == ""
}

type DocsRepoID string

type DocsSnapshot []byte

type DocsReadRepository interface {
	GetHead(ctx context.Context, project ProjectName) (CommitHash, error)
	GetSnapshot(ctx context.Context, project ProjectName, commit CommitHash) (DocsSnapshot, CommitHash, error)
}

// DocsPushStatus represents the result status of a docs push operation.
type DocsPushStatus string

const (
	DocsPushStatusUpdated  DocsPushStatus = "updated"
	DocsPushStatusNoChange DocsPushStatus = "nochange"
	DocsPushStatusOutdated DocsPushStatus = "outdated"
)

// DocsPushResult represents the result of a docs push operation.
type DocsPushResult struct {
	Status      DocsPushStatus
	NewHead     *CommitHash // Only set when Status is updated
	CurrentHead CommitHash  // Set when Status is nochange or outdated
}

// DocsWriteRepository defines operations for writing to docs repositories.
type DocsWriteRepository interface {
	PushSnapshot(ctx context.Context, project ProjectName, base CommitHash, snapshot DocsSnapshot, actorEmail string) (DocsPushResult, error)
	Push(ctx context.Context, project ProjectName) error
	Reset(ctx context.Context, project ProjectName) error
}
