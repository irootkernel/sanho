package git

import (
	"context"
	"sync"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

// RepoCoordinator serializes every operation that can read or mutate a docs
// repository clone. A single coordinator must be shared by the server's Git
// adapters and docs push use case.
type RepoCoordinator struct {
	mu    sync.Mutex
	locks map[docs.DocsRepoID]chan struct{}
}

func NewRepoCoordinator() *RepoCoordinator {
	return &RepoCoordinator{
		locks: make(map[docs.DocsRepoID]chan struct{}),
	}
}

func (c *RepoCoordinator) lockFor(repoID docs.DocsRepoID) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	lock, ok := c.locks[repoID]
	if !ok {
		lock = make(chan struct{}, 1)
		c.locks[repoID] = lock
	}
	return lock
}

func (c *RepoCoordinator) Lock(ctx context.Context, repoID docs.DocsRepoID) error {
	select {
	case c.lockFor(repoID) <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *RepoCoordinator) TryLock(repoID docs.DocsRepoID) bool {
	select {
	case c.lockFor(repoID) <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *RepoCoordinator) Unlock(repoID docs.DocsRepoID) {
	<-c.lockFor(repoID)
}
