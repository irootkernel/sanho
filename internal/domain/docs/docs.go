package docs

import "context"

type ProjectName string
type CommitHash string

func (h CommitHash) IsZero() bool {
	return h == ""
}

type DocsRepoID string

type DocsReadRepository interface {
	GetHead(ctx context.Context, project ProjectName) (CommitHash, error)
}
