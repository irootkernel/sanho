package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
)

func reconcileWorkspaceDocsFromHEAD(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
) (bool, error) {
	config.ApplyDefaults()
	gitClient := infraGit.NewClient()
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	isRepository, err := syncer.IsRepository(ctx, workDir)
	if err != nil || !isRepository {
		return false, err
	}
	if err := requireWorkspaceMutationSafe(ctx, workDir); err != nil {
		return false, err
	}
	head, err := syncer.Head(ctx, workDir)
	if err != nil {
		return false, err
	}

	pulledStore, err := newPullCommitEngine(nil).pulledDocsStore(ctx, workDir)
	if err != nil {
		return false, err
	}
	pulledState, hasPulledDocs, err := pulledStore.Load()
	if err != nil {
		return false, err
	}
	if hasPulledDocs && head == pulledState.OriginalHead {
		return false, nil
	}

	version, found, err := gitClient.ResolveHeadDocsVersion(ctx, workDir, config.DocsDir)
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("current HEAD docs tree does not match a reachable docs-version commit")
	}
	if hasPulledDocs {
		dirty, err := gitClient.HasLocalDocsChanges(
			ctx,
			workDir,
			config.DocsDir,
		)
		if err != nil {
			return false, err
		}
		if dirty {
			return false, fmt.Errorf(
				"HEAD changed while pulled docs were pending and local docs are dirty; run 'sanho pull-commit' or restore the previous HEAD",
			)
		}
	}

	hashPath := filepath.Join(workDir, config.DocsHashFile)
	currentHash, err := fs.NewFileDocsHashStore().Read(hashPath)
	if err != nil {
		return false, err
	}
	if currentHash == version.DocsHash {
		if hasPulledDocs {
			if err := pulledStore.Remove(); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if err := fs.NewFileDocsHashStore().Write(hashPath, version.DocsHash); err != nil {
		return false, fmt.Errorf("update docs hash from HEAD: %w", err)
	}
	if err := reportWorkspaceDocsHash(ctx, workDir, config, version.DocsHash); err != nil {
		return true, err
	}
	if hasPulledDocs {
		if err := pulledStore.Remove(); err != nil {
			return true, err
		}
	}
	return true, nil
}
