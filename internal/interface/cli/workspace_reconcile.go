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

var errHeadDocsProvenanceMissing = errors.New("current HEAD docs tree does not match a reachable docs-version commit")

func reconcileWorkspaceDocsFromHEADWithPermit(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	permit workspaceMutationPermit,
) (bool, error) {
	return reconcileWorkspaceDocsFromHEADWithVerifier(ctx, workDir, config, permit, nil)
}

func reconcileWorkspaceDocsFromHEADWithVerifier(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	permit workspaceMutationPermit,
	verifier *docsProvenanceVerifier,
) (bool, error) {
	config.ApplyDefaults()
	gitClient := infraGit.NewClient()
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	isRepository, err := syncer.IsRepository(ctx, workDir)
	if err != nil || !isRepository {
		return false, err
	}
	if err := requireWorkspaceMutationSafeWithPermit(ctx, workDir, permit); err != nil {
		return false, err
	}
	unmerged, err := gitClient.HasUnmergedEntries(ctx, workDir)
	if err != nil {
		return false, err
	}
	if unmerged {
		return false, errors.New("git index contains unmerged entries; resolve conflicts before Sanho reconciliation")
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

	_, found, err := gitClient.ResolveDocsVersionCandidate(ctx, workDir, head, config.DocsDir)
	if err != nil {
		return false, err
	}
	if !found {
		return false, errHeadDocsProvenanceMissing
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
	if verifier == nil {
		rawHTTPClient, err := newDaemonClient(config.SocketPath)
		if err != nil {
			return false, err
		}
		verifier = newDocsProvenanceVerifier(rawHTTPClient)
	}
	provenance, err := verifier.Verify(ctx, workDir, head, config)
	if err != nil {
		return false, err
	}
	if !provenance.Valid {
		return false, fmt.Errorf("current HEAD docs provenance is %s: %s", provenance.Classification, provenance.Reason)
	}
	if currentHash == provenance.DocsHash {
		if hasPulledDocs {
			if err := pulledStore.Remove(); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if err := fs.NewFileDocsHashStore().Write(hashPath, provenance.DocsHash); err != nil {
		return false, fmt.Errorf("update docs hash from HEAD: %w", err)
	}
	if err := reportWorkspaceDocsHashWithPermit(ctx, workDir, config, provenance.DocsHash, permit); err != nil {
		return true, err
	}
	if hasPulledDocs {
		if err := pulledStore.Remove(); err != nil {
			return true, err
		}
	}
	return true, nil
}
