package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
)

type mainPublicationClassification string

const (
	mainPublicationPending mainPublicationClassification = "pending"
	mainPublicationBlocked mainPublicationClassification = "blocked"
	mainPublicationCorrupt mainPublicationClassification = "corrupt"
)

type mainPublicationAssessment struct {
	State          fs.MainPublicationState
	Exists         bool
	Classification mainPublicationClassification
	Reason         string
	LocalMain      string
	RemoteMain     string
}

func assessMainPublication(
	ctx context.Context,
	workDir string,
	refreshOrigin bool,
) (mainPublicationAssessment, error) {
	workspaceSync := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	path, err := workspaceSync.ResolveMainPublicationPath(ctx, workDir)
	if err != nil {
		return mainPublicationAssessment{}, err
	}
	store := fs.NewMainPublicationStore(path)
	state, exists, err := store.Load()
	if err != nil || !exists {
		return mainPublicationAssessment{Exists: exists}, err
	}
	assessment := mainPublicationAssessment{
		State:          state,
		Exists:         true,
		Classification: mainPublicationPending,
	}

	for _, recorded := range state.Commits {
		parents, err := workspaceSync.CommitParents(ctx, workDir, recorded.Commit)
		if err != nil {
			assessment.Classification = mainPublicationCorrupt
			assessment.Reason = fmt.Sprintf("resolve recorded commit %s: %v", recorded.Commit, err)
			return assessment, nil
		}
		if len(parents) != 1 || parents[0] != recorded.Parent {
			assessment.Classification = mainPublicationCorrupt
			assessment.Reason = fmt.Sprintf("recorded commit %s no longer has its expected parent", recorded.Commit)
			return assessment, nil
		}
		valid, err := workspaceSync.IsDocsSyncCommit(
			ctx,
			workDir,
			recorded.Commit,
			recorded.Parent,
			recorded.Subject,
			recorded.DocsHash,
		)
		if err != nil {
			return assessment, err
		}
		if !valid {
			assessment.Classification = mainPublicationCorrupt
			assessment.Reason = fmt.Sprintf("recorded commit %s is not the expected docs sync commit", recorded.Commit)
			return assessment, nil
		}
	}

	localMain, err := workspaceSync.ResolveRef(ctx, workDir, "refs/heads/main")
	if err != nil {
		assessment.Classification = mainPublicationBlocked
		assessment.Reason = "local main branch does not exist"
		return assessment, nil
	}
	assessment.LocalMain = localMain
	for _, recorded := range state.Commits {
		contains, err := workspaceSync.IsAncestor(ctx, workDir, recorded.Commit, localMain)
		if err != nil {
			return assessment, err
		}
		if !contains {
			assessment.Classification = mainPublicationCorrupt
			assessment.Reason = fmt.Sprintf("local main no longer contains recorded docs sync commit %s", recorded.Commit)
			return assessment, nil
		}
	}

	if refreshOrigin {
		if err := fetchOriginMain(ctx, workDir); err != nil {
			assessment.Classification = mainPublicationBlocked
			assessment.Reason = err.Error()
			return assessment, nil
		}
	}
	remoteMain, remoteExists, err := workspaceSync.ResolveOptionalRef(ctx, workDir, "refs/remotes/origin/main")
	if err != nil {
		return assessment, err
	}
	if !remoteExists {
		assessment.Classification = mainPublicationBlocked
		assessment.Reason = "origin/main does not exist"
		return assessment, nil
	}
	assessment.RemoteMain = remoteMain

	allPublished := true
	for _, recorded := range state.Commits {
		contains, err := workspaceSync.IsAncestor(ctx, workDir, recorded.Commit, remoteMain)
		if err != nil {
			return assessment, err
		}
		if !contains {
			allPublished = false
			break
		}
	}
	if allPublished {
		if err := store.Remove(); err != nil {
			return assessment, err
		}
		return mainPublicationAssessment{}, nil
	}

	fastForward, err := workspaceSync.IsAncestor(ctx, workDir, remoteMain, localMain)
	if err != nil {
		return assessment, err
	}
	if !fastForward {
		assessment.Classification = mainPublicationBlocked
		assessment.Reason = "local main has diverged from origin/main"
		return assessment, nil
	}
	assessment.Reason = "local main contains commits that are pending publication to origin/main"
	return assessment, nil
}

func fetchOriginMain(ctx context.Context, workDir string) error {
	if err := runMainPublicationGit(ctx, workDir, "remote", "get-url", "origin"); err != nil {
		return fmt.Errorf("origin remote is unavailable: %w", err)
	}
	if err := runMainPublicationGit(ctx, workDir, "fetch", "--no-tags", "origin", "main"); err != nil {
		return fmt.Errorf("fetch origin/main: %w", err)
	}
	return nil
}

func pushLocalMain(ctx context.Context, workDir string) error {
	if err := runMainPublicationGit(
		ctx,
		workDir,
		"push",
		"origin",
		"refs/heads/main:refs/heads/main",
	); err != nil {
		return fmt.Errorf("publish local main to origin/main: %w", err)
	}
	return nil
}

func runMainPublicationGit(ctx context.Context, workDir string, args ...string) error {
	commandArgs := append([]string{"-C", workDir}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return err
		}
		return errors.Join(err, errors.New(message))
	}
	return nil
}
