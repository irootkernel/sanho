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
