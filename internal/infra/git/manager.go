package git

import (
	"context"
	"fmt"
	"os"

	"github.com/SeventeenthEarth/kkachi/internal/config"
)

type DocsRepoManager struct {
	client *Client
}

func NewDocsRepoManager(client *Client) *DocsRepoManager {
	return &DocsRepoManager{
		client: client,
	}
}

func (m *DocsRepoManager) Sync(ctx context.Context, repos []config.DocsRepoConfig) error {
	for _, repo := range repos {
		if _, err := os.Stat(repo.Path); os.IsNotExist(err) {
			if err := m.client.Clone(ctx, repo.RepoURL, repo.Path); err != nil {
				return fmt.Errorf("failed to clone %s: %w", repo.ID, err)
			}
		} else {
			if err := m.client.Pull(ctx, repo.Path); err != nil {
				return fmt.Errorf("failed to pull %s: %w", repo.ID, err)
			}
		}
		// Configure git user for commits (needed for push operations)
		// Called for both new clones and existing repos to ensure config is always set
		if err := m.client.ConfigUser(ctx, repo.Path, "kkachi-server@local", "kkachi-server"); err != nil {
			return fmt.Errorf("failed to configure git user for %s: %w", repo.ID, err)
		}
	}
	return nil
}

func (m *DocsRepoManager) DeleteRepo(path string) error {
	return os.RemoveAll(path)
}
