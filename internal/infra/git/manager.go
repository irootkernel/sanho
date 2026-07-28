package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

type DocsRepoManager struct {
	client      *Client
	coordinator *RepoCoordinator
}

func NewDocsRepoManager(client *Client, coordinator *RepoCoordinator) *DocsRepoManager {
	return &DocsRepoManager{
		client:      client,
		coordinator: coordinator,
	}
}

func (m *DocsRepoManager) Sync(ctx context.Context, repos []config.DocsRepoConfig) error {
	for _, repo := range repos {
		repoID := docs.DocsRepoID(repo.ID)
		if err := m.coordinator.Lock(ctx, repoID); err != nil {
			return fmt.Errorf("failed to lock %s: %w", repo.ID, err)
		}

		err := m.syncRepo(ctx, repo)
		m.coordinator.Unlock(repoID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *DocsRepoManager) syncRepo(ctx context.Context, repo config.DocsRepoConfig) error {
	if _, err := os.Stat(repo.Path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(repo.Path), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", repo.ID, err)
		}
		if err := m.client.Clone(ctx, repo.RepoURL, repo.Path); err != nil {
			return fmt.Errorf("failed to clone %s: %w", repo.ID, err)
		}
	} else {
		if err := refreshRepo(ctx, m.client, repo.Path); err != nil {
			return fmt.Errorf("failed to refresh %s: %w", repo.ID, err)
		}
	}
	if err := m.client.ConfigUser(ctx, repo.Path, "kkachi-server@local", "kkachi-server"); err != nil {
		return fmt.Errorf("failed to configure git user for %s: %w", repo.ID, err)
	}
	return nil
}

func (m *DocsRepoManager) DeleteRepo(ctx context.Context, repoID string, path string) error {
	id := docs.DocsRepoID(repoID)
	if err := m.coordinator.Lock(ctx, id); err != nil {
		return err
	}
	defer m.coordinator.Unlock(id)
	return os.RemoveAll(path)
}
