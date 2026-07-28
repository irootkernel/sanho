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

// CommitRelationStatus describes how one commit relates to another commit.
type CommitRelationStatus string

const (
	CommitRelationSame     CommitRelationStatus = "same"
	CommitRelationAhead    CommitRelationStatus = "ahead"
	CommitRelationBehind   CommitRelationStatus = "behind"
	CommitRelationDiverged CommitRelationStatus = "diverged"
	CommitRelationUnknown  CommitRelationStatus = "unknown"
)

// CommitRelation reports commits reachable only from the compared commit
// (Ahead) and only from the target commit (Behind).
type CommitRelation struct {
	Status CommitRelationStatus
	Ahead  int
	Behind int
}

// CommitComparison contains a workspace commit's relationship to the caller's
// reference commit and to the current docs HEAD.
type CommitComparison struct {
	RelativeToReference CommitRelation
	RelativeToHead      CommitRelation
}

// ProjectCommitComparison is a consistent comparison snapshot for one project.
type ProjectCommitComparison struct {
	Head                 CommitHash
	ReferenceToHead      CommitRelation
	WorkspaceComparisons map[CommitHash]CommitComparison
}

// DocsStatusRepository compares workspace commits in a docs repository.
type DocsStatusRepository interface {
	CompareProjectCommits(
		ctx context.Context,
		project ProjectName,
		reference CommitHash,
		workspaceCommits []CommitHash,
	) (ProjectCommitComparison, error)
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
