package git_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
)

func TestRepoCoordinatorSerializesSameRepo(t *testing.T) {
	coordinator := git.NewRepoCoordinator()
	repoID := docs.DocsRepoID("docs")

	if !coordinator.TryLock(repoID) {
		t.Fatal("first lock was not acquired")
	}
	if coordinator.TryLock(repoID) {
		t.Fatal("second lock for the same repo was acquired")
	}

	coordinator.Unlock(repoID)
	if !coordinator.TryLock(repoID) {
		t.Fatal("lock was not available after unlock")
	}
	coordinator.Unlock(repoID)
}

func TestRepoCoordinatorLockHonorsContext(t *testing.T) {
	coordinator := git.NewRepoCoordinator()
	repoID := docs.DocsRepoID("docs")
	if !coordinator.TryLock(repoID) {
		t.Fatal("first lock was not acquired")
	}
	defer coordinator.Unlock(repoID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := coordinator.Lock(ctx, repoID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock() error = %v, want deadline exceeded", err)
	}
}

func TestRepoCoordinatorAllowsDifferentRepos(t *testing.T) {
	coordinator := git.NewRepoCoordinator()
	if !coordinator.TryLock("docs-a") {
		t.Fatal("docs-a lock was not acquired")
	}
	defer coordinator.Unlock("docs-a")
	if !coordinator.TryLock("docs-b") {
		t.Fatal("docs-b should not be blocked by docs-a")
	}
	coordinator.Unlock("docs-b")
}
