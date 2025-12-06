package docs

import (
	"sync"

	domain "github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

// DocsRepoMutexManager manages mutex locks for docs repositories to prevent concurrent pushes.
type DocsRepoMutexManager interface {
	TryLock(docsRepoID domain.DocsRepoID) bool
	Unlock(docsRepoID domain.DocsRepoID)
}

// InMemoryMutexManager is an in-memory implementation of DocsRepoMutexManager.
type InMemoryMutexManager struct {
	mu    sync.Mutex
	locks map[domain.DocsRepoID]struct{}
}

func NewInMemoryMutexManager() *InMemoryMutexManager {
	return &InMemoryMutexManager{
		locks: make(map[domain.DocsRepoID]struct{}),
	}
}

// TryLock attempts to acquire a lock for the given docs repo.
// Returns true if the lock was acquired, false if the repo is already locked.
func (m *InMemoryMutexManager) TryLock(docsRepoID domain.DocsRepoID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.locks[docsRepoID]; exists {
		return false
	}
	m.locks[docsRepoID] = struct{}{}
	return true
}

// Unlock releases the lock for the given docs repo.
func (m *InMemoryMutexManager) Unlock(docsRepoID domain.DocsRepoID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locks, docsRepoID)
}
