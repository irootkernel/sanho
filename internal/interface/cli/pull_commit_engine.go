package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
	"github.com/irootkernel/sanho/internal/usecase/hook"
)

var (
	errPullCommitConflict = errors.New("pull-commit has conflicts")
	errPullCommitRetry    = errors.New("docs base commit created; retry the original commit")
)

type pullCommitHTTPClient interface {
	DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error)
	DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error)
	ReportWorkspaceDocsHash(
		ctx context.Context,
		workspaceID workspace.WorkspaceID,
		req httpclient.ReportWorkspaceDocsHashRequest,
	) error
}

type pullCommitEngine struct {
	httpClient       pullCommitHTTPClient
	gitClient        *infraGit.Client
	workspaceSync    *infraGit.WorkspaceSync
	snapshotBuilder  *fs.SnapshotBuilder
	snapshotApplier  *fs.SnapshotApplier
	docsHashStore    *fs.FileDocsHashStore
	conflictDetector *merge.FileConflictDetector
	recoveryStep     func(string) error
}

func newPullCommitEngine(httpClient pullCommitHTTPClient) *pullCommitEngine {
	builder := fs.NewSnapshotBuilder()
	applier := fs.NewSnapshotApplier()
	return &pullCommitEngine{
		httpClient:       httpClient,
		gitClient:        infraGit.NewClient(),
		workspaceSync:    infraGit.NewWorkspaceSync(builder, applier),
		snapshotBuilder:  builder,
		snapshotApplier:  applier,
		docsHashStore:    fs.NewFileDocsHashStore(),
		conflictDetector: merge.NewFileConflictDetector(),
	}
}

func (e *pullCommitEngine) store(ctx context.Context, workDir string) (*fs.PullCommitStore, error) {
	dir, err := e.workspaceSync.ResolveTransactionDir(ctx, workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve pull-commit state directory: %w", err)
	}
	return fs.NewPullCommitStore(dir), nil
}

func (e *pullCommitEngine) pulledDocsStore(ctx context.Context, workDir string) (*fs.PulledDocsStore, error) {
	dir, err := e.workspaceSync.ResolvePulledDocsDir(ctx, workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve pulled docs state directory: %w", err)
	}
	return fs.NewPulledDocsStore(dir), nil
}

func (e *pullCommitEngine) hasPulledDocs(ctx context.Context, workDir string) (bool, error) {
	store, err := e.pulledDocsStore(ctx, workDir)
	if err != nil {
		return false, err
	}
	_, exists, err := store.Load()
	return exists, err
}

func (e *pullCommitEngine) pulledDocsHaveLocalChanges(
	ctx context.Context,
	workDir, docsDir string,
) (bool, error) {
	store, err := e.pulledDocsStore(ctx, workDir)
	if err != nil {
		return false, err
	}
	_, exists, err := store.Load()
	if err != nil {
		return false, err
	}
	if !exists {
		return e.gitClient.HasLocalDocsChanges(ctx, workDir, docsDir)
	}
	originalIndex, err := store.ReadArtifact(fs.PulledDocsOriginalIndexSnapshot)
	if err != nil {
		return false, err
	}
	adoptedWork, err := store.ReadArtifact(fs.PulledDocsAdoptedSnapshot)
	if err != nil {
		return false, err
	}
	currentIndex, err := e.workspaceSync.BuildIndexDocsSnapshot(ctx, workDir, docsDir)
	if err != nil {
		return false, err
	}
	currentWork, err := e.snapshotBuilder.Build(filepath.Join(workDir, docsDir))
	if err != nil {
		return false, err
	}
	indexEqual, err := e.snapshotsEqual(docsDir, originalIndex, currentIndex)
	if err != nil {
		return false, err
	}
	workEqual, err := e.snapshotsEqual(docsDir, adoptedWork, currentWork)
	if err != nil {
		return false, err
	}
	return !indexEqual || !workEqual, nil
}

func (e *pullCommitEngine) hasTransaction(ctx context.Context, workDir string) (bool, error) {
	isRepository, err := e.workspaceSync.IsRepository(ctx, workDir)
	if err != nil {
		return false, err
	}
	if !isRepository {
		return false, nil
	}
	store, err := e.store(ctx, workDir)
	if err != nil {
		return false, err
	}
	state, exists, err := store.Load()
	if err != nil || !exists {
		return exists, err
	}
	if state.Phase != fs.PullCommitPhasePrepared {
		return true, nil
	}
	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return true, err
	}
	preparedHead := pullCommitPreparedHead(state)
	if head == preparedHead {
		return true, nil
	}
	completed, err := e.workspaceSync.IsAncestor(ctx, workDir, preparedHead, head)
	if err != nil {
		return true, err
	}
	if !completed {
		return true, nil
	}
	if err := store.Remove(); err != nil {
		return true, err
	}
	return false, nil
}

func (e *pullCommitEngine) start(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	baseHash, remoteHash docs.CommitHash,
) (fs.PullCommitState, error) {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return fs.PullCommitState{}, err
	}
	exists, err := store.Exists()
	if err != nil {
		return fs.PullCommitState{}, err
	}
	if exists {
		return fs.PullCommitState{}, errors.New("a pull-commit transaction already exists")
	}

	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return fs.PullCommitState{}, fmt.Errorf("resolve current HEAD: %w", err)
	}
	branchRef, err := e.workspaceSync.SymbolicHead(ctx, workDir)
	if err != nil {
		return fs.PullCommitState{}, err
	}
	transactionID, err := newPullCommitTransactionID()
	if err != nil {
		return fs.PullCommitState{}, err
	}
	baseSnapshot, _, err := e.httpClient.DocsSnapshot(ctx, config.Project, baseHash)
	if err != nil {
		return fs.PullCommitState{}, fmt.Errorf("download base docs snapshot: %w", err)
	}
	remoteSnapshot, actualRemoteHash, err := e.httpClient.DocsSnapshot(ctx, config.Project, remoteHash)
	if err != nil {
		return fs.PullCommitState{}, fmt.Errorf("download remote docs snapshot: %w", err)
	}
	if !actualRemoteHash.IsZero() {
		remoteHash = actualRemoteHash
	}
	indexSnapshot, err := e.workspaceSync.BuildIndexDocsSnapshot(ctx, workDir, config.DocsDir)
	if err != nil {
		return fs.PullCommitState{}, fmt.Errorf("capture staged docs: %w", err)
	}
	pulledStore, err := e.pulledDocsStore(ctx, workDir)
	if err != nil {
		return fs.PullCommitState{}, err
	}
	pulledState, hasPulledDocs, err := pulledStore.Load()
	if err != nil {
		return fs.PullCommitState{}, err
	}
	if hasPulledDocs {
		if pulledState.AdoptedHash != baseHash {
			return fs.PullCommitState{}, fmt.Errorf(
				"pulled docs baseline is %s but local docs hash is %s",
				pulledState.AdoptedHash,
				baseHash,
			)
		}
		originalIndex, err := pulledStore.ReadArtifact(fs.PulledDocsOriginalIndexSnapshot)
		if err != nil {
			return fs.PullCommitState{}, err
		}
		indexSnapshot, err = e.overlayChangedSnapshotPaths(
			config.DocsDir,
			baseSnapshot,
			originalIndex,
			indexSnapshot,
		)
		if err != nil {
			return fs.PullCommitState{}, fmt.Errorf("normalize staged docs after pull: %w", err)
		}
	}
	workSnapshot, err := e.snapshotBuilder.Build(filepath.Join(workDir, config.DocsDir))
	if err != nil {
		return fs.PullCommitState{}, fmt.Errorf("capture working docs: %w", err)
	}

	mergedIndex, mergedWork, conflictFiles, err := e.mergeSnapshots(
		ctx,
		config.DocsDir,
		baseSnapshot,
		indexSnapshot,
		workSnapshot,
		remoteSnapshot,
	)
	if err != nil {
		return fs.PullCommitState{}, err
	}

	artifacts := map[string][]byte{
		fs.PullCommitBaseSnapshot:          baseSnapshot,
		fs.PullCommitOriginalIndexSnapshot: indexSnapshot,
		fs.PullCommitOriginalWorkSnapshot:  workSnapshot,
		fs.PullCommitMergedIndexSnapshot:   mergedIndex,
		fs.PullCommitMergedWorkSnapshot:    mergedWork,
		fs.PullCommitRemoteSnapshot:        remoteSnapshot,
	}
	for name, data := range artifacts {
		if err := store.WriteArtifact(name, data); err != nil {
			_ = store.Remove()
			return fs.PullCommitState{}, err
		}
	}

	state := fs.PullCommitState{
		Version:       3,
		Phase:         fs.PullCommitPhaseReady,
		TransactionID: transactionID,
		BranchRef:     branchRef,
		OriginalHead:  head,
		BaseHash:      baseHash,
		RemoteHash:    remoteHash,
		ConflictFiles: conflictFiles,
		CreatedAt:     time.Now(),
	}
	if err := store.Save(state); err != nil {
		_ = store.Remove()
		return fs.PullCommitState{}, err
	}
	if err := e.workspaceSync.ApplyWorktreeDocsSnapshot(workDir, config.DocsDir, mergedWork); err != nil {
		return state, err
	}
	if len(conflictFiles) > 0 {
		state.Phase = fs.PullCommitPhaseConflict
		if err := store.Save(state); err != nil {
			return state, err
		}
		return state, errPullCommitConflict
	}
	return e.createSyncCommit(ctx, workDir, config, store, state)
}

func (e *pullCommitEngine) restartAfterOutdated(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	baseHash, remoteHash docs.CommitHash,
) (fs.PullCommitState, error) {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return fs.PullCommitState{}, err
	}
	oldState, exists, err := store.Load()
	if err != nil {
		return fs.PullCommitState{}, err
	}
	if !exists {
		return e.start(ctx, workDir, config, baseHash, remoteHash)
	}
	if oldState.Phase != fs.PullCommitPhasePrepared {
		return oldState, fmt.Errorf("cannot advance pull-commit from phase %s", oldState.Phase)
	}
	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return oldState, err
	}
	if head != pullCommitPreparedHead(oldState) {
		return oldState, fmt.Errorf(
			"HEAD changed before repeated docs sync; expected %s, got %s",
			pullCommitPreparedHead(oldState),
			head,
		)
	}

	artifactNames := []string{
		fs.PullCommitBaseSnapshot,
		fs.PullCommitOriginalIndexSnapshot,
		fs.PullCommitOriginalWorkSnapshot,
		fs.PullCommitMergedIndexSnapshot,
		fs.PullCommitMergedWorkSnapshot,
		fs.PullCommitRemoteSnapshot,
	}
	oldArtifacts := make(map[string][]byte, len(artifactNames))
	for _, name := range artifactNames {
		data, err := store.ReadArtifact(name)
		if err != nil {
			return oldState, err
		}
		oldArtifacts[name] = data
	}
	if err := store.Remove(); err != nil {
		return oldState, err
	}

	newState, startErr := e.start(ctx, workDir, config, baseHash, remoteHash)
	if startErr == nil || errors.Is(startErr, errPullCommitConflict) || errors.Is(startErr, errPullCommitRetry) {
		return newState, startErr
	}
	newExists, existsErr := store.Exists()
	if existsErr != nil {
		return newState, errors.Join(startErr, existsErr)
	}
	if newExists {
		return newState, startErr
	}
	for _, name := range artifactNames {
		if err := store.WriteArtifact(name, oldArtifacts[name]); err != nil {
			return newState, errors.Join(startErr, fmt.Errorf("restore previous pull-commit artifact: %w", err))
		}
	}
	if err := store.Save(oldState); err != nil {
		return newState, errors.Join(startErr, fmt.Errorf("restore previous pull-commit state: %w", err))
	}
	return oldState, startErr
}

func (e *pullCommitEngine) resume(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
) (fs.PullCommitState, bool, error) {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return fs.PullCommitState{}, false, err
	}
	state, exists, err := store.Load()
	if err != nil {
		return fs.PullCommitState{}, false, err
	}
	if !exists {
		return fs.PullCommitState{}, false, nil
	}

	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return state, true, err
	}
	switch state.Phase {
	case fs.PullCommitPhaseConflict:
		if head != state.OriginalHead {
			return state, true, fmt.Errorf("HEAD changed during conflict resolution; expected %s, got %s", state.OriginalHead, head)
		}
		conflicts, err := e.conflictDetector.DetectConflicts(filepath.Join(workDir, config.DocsDir))
		if err != nil {
			return state, true, fmt.Errorf("check resolved conflicts: %w", err)
		}
		if len(conflicts) > 0 {
			state.ConflictFiles = conflicts
			_ = store.Save(state)
			return state, true, errPullCommitConflict
		}
		paths := make([]string, 0, len(state.ConflictFiles))
		for _, path := range state.ConflictFiles {
			paths = append(paths, filepath.Join(config.DocsDir, path))
		}
		differs, err := e.workspaceSync.PathsDifferFromIndex(ctx, workDir, paths)
		if err != nil {
			return state, true, err
		}
		if differs {
			return state, true, errors.New("resolved docs conflicts must be staged before continuing")
		}
		currentIndex, err := e.workspaceSync.BuildIndexDocsSnapshot(ctx, workDir, config.DocsDir)
		if err != nil {
			return state, true, fmt.Errorf("capture resolved staged docs: %w", err)
		}
		mergedIndex, err := store.ReadArtifact(fs.PullCommitMergedIndexSnapshot)
		if err != nil {
			return state, true, err
		}
		indexSnapshot, err := e.overlaySelectedSnapshotPaths(
			config.DocsDir,
			mergedIndex,
			currentIndex,
			state.ConflictFiles,
		)
		if err != nil {
			return state, true, fmt.Errorf("apply resolved staged docs: %w", err)
		}
		workSnapshot, err := e.snapshotBuilder.Build(filepath.Join(workDir, config.DocsDir))
		if err != nil {
			return state, true, fmt.Errorf("capture resolved working docs: %w", err)
		}
		if err := store.WriteArtifact(fs.PullCommitMergedIndexSnapshot, indexSnapshot); err != nil {
			return state, true, err
		}
		if err := store.WriteArtifact(fs.PullCommitMergedWorkSnapshot, workSnapshot); err != nil {
			return state, true, err
		}
		state.Phase = fs.PullCommitPhaseReady
		state.ConflictFiles = nil
		if err := store.Save(state); err != nil {
			return state, true, err
		}
		created, err := e.createSyncCommit(ctx, workDir, config, store, state)
		return created, true, err

	case fs.PullCommitPhaseReady:
		if head != state.OriginalHead {
			mainHead, mainErr := e.workspaceSync.ResolveRef(ctx, workDir, "refs/heads/main")
			if mainErr != nil {
				return state, true, mainErr
			}
			recovered, err := e.workspaceSync.IsDocsSyncCommit(
				ctx,
				workDir,
				mainHead,
				"",
				config.DocsSyncCommitMessage,
				string(state.RemoteHash),
			)
			if err != nil {
				return state, true, err
			}
			if !recovered {
				return state, true, fmt.Errorf("HEAD changed while preparing docs sync; expected %s, got %s", state.OriginalHead, head)
			}
			descendant, err := e.workspaceSync.IsAncestor(ctx, workDir, mainHead, head)
			if err != nil {
				return state, true, err
			}
			if !descendant {
				return state, true, fmt.Errorf("current branch does not contain recovered main docs sync commit %s", mainHead)
			}
			state.Phase = fs.PullCommitPhaseSyncCommitted
			state.SyncCommit = mainHead
			state.PreparedHead = head
			if err := store.Save(state); err != nil {
				return state, true, err
			}
			if err := e.prepare(ctx, workDir, config, store, &state); err != nil {
				return state, true, err
			}
			return state, true, errPullCommitRetry
		}
		if len(state.ConflictFiles) > 0 {
			state.Phase = fs.PullCommitPhaseConflict
			if err := store.Save(state); err != nil {
				return state, true, err
			}
			return state, true, errPullCommitConflict
		}
		mergedWork, err := store.ReadArtifact(fs.PullCommitMergedWorkSnapshot)
		if err != nil {
			return state, true, err
		}
		if err := e.workspaceSync.ApplyWorktreeDocsSnapshot(workDir, config.DocsDir, mergedWork); err != nil {
			return state, true, err
		}
		created, err := e.createSyncCommit(ctx, workDir, config, store, state)
		return created, true, err

	case fs.PullCommitPhasePrepared:
		preparedHead := pullCommitPreparedHead(state)
		if head != preparedHead {
			completed, err := e.workspaceSync.IsAncestor(ctx, workDir, preparedHead, head)
			if err != nil {
				return state, true, err
			}
			if completed {
				if err := store.Remove(); err != nil {
					return state, true, err
				}
				return fs.PullCommitState{}, false, nil
			}
			return state, true, fmt.Errorf("HEAD changed after docs sync; expected descendant of %s, got %s", preparedHead, head)
		}
		if err := e.prepare(ctx, workDir, config, store, &state); err != nil {
			return state, true, err
		}
		return state, true, nil

	case fs.PullCommitPhaseSyncCommitted:
		if head != pullCommitPreparedHead(state) {
			return state, true, fmt.Errorf(
				"HEAD changed after docs sync commit; expected %s, got %s",
				pullCommitPreparedHead(state),
				head,
			)
		}
		if err := e.prepare(ctx, workDir, config, store, &state); err != nil {
			return state, true, err
		}
		return state, true, nil

	default:
		return state, true, fmt.Errorf("unknown pull-commit phase: %s", state.Phase)
	}
}

func (e *pullCommitEngine) createSyncCommit(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	store *fs.PullCommitStore,
	state fs.PullCommitState,
) (fs.PullCommitState, error) {
	remoteSnapshot, err := store.ReadArtifact(fs.PullCommitRemoteSnapshot)
	if err != nil {
		return state, err
	}
	result, err := e.workspaceSync.CreateMainBasedDocsSyncCommit(
		ctx,
		workDir,
		config.DocsDir,
		remoteSnapshot,
		config.DocsSyncCommitMessage,
		string(state.RemoteHash),
	)
	if err != nil {
		return state, err
	}
	state.Phase = fs.PullCommitPhaseSyncCommitted
	state.SyncCommit = result.SyncCommit
	state.PreparedHead = result.PreparedHead
	if err := store.Save(state); err != nil {
		return state, err
	}
	if err := e.prepare(ctx, workDir, config, store, &state); err != nil {
		return state, err
	}
	pulledStore, err := e.pulledDocsStore(ctx, workDir)
	if err != nil {
		return state, err
	}
	if _, exists, err := pulledStore.Load(); err != nil {
		return state, err
	} else if exists {
		if err := pulledStore.Remove(); err != nil {
			return state, err
		}
	}
	return state, errPullCommitRetry
}

func (e *pullCommitEngine) prepare(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	store *fs.PullCommitStore,
	state *fs.PullCommitState,
) error {
	if err := e.writeRemoteHash(workDir, config, state.RemoteHash); err != nil {
		return err
	}
	mergedIndex, err := store.ReadArtifact(fs.PullCommitMergedIndexSnapshot)
	if err != nil {
		return err
	}
	mergedWork, err := store.ReadArtifact(fs.PullCommitMergedWorkSnapshot)
	if err != nil {
		return err
	}
	if err := e.workspaceSync.StageDocsSnapshot(ctx, workDir, config.DocsDir, mergedIndex); err != nil {
		return err
	}
	preparedTree, err := e.workspaceSync.WriteTree(ctx, workDir)
	if err != nil {
		return err
	}
	state.Version = 3
	state.PreparedTree = preparedTree
	if state.BranchRef == "" {
		state.BranchRef, err = e.workspaceSync.SymbolicHead(ctx, workDir)
		if err != nil {
			return err
		}
	}
	if err := e.workspaceSync.ApplyWorktreeDocsSnapshot(workDir, config.DocsDir, mergedWork); err != nil {
		return err
	}
	if err := e.reportRemoteHash(ctx, config, store, state); err != nil {
		return err
	}
	state.Phase = fs.PullCommitPhasePrepared
	return store.Save(*state)
}

func (e *pullCommitEngine) reportRemoteHash(
	ctx context.Context,
	config *client.WorkspaceConfig,
	store *fs.PullCommitStore,
	state *fs.PullCommitState,
) error {
	if state.Reported {
		return nil
	}
	if e.httpClient == nil {
		return errors.New("workspace docs hash reporter is unavailable")
	}
	if err := e.httpClient.ReportWorkspaceDocsHash(
		ctx,
		config.WorkspaceID,
		httpclient.ReportWorkspaceDocsHashRequest{
			DocsHash:   state.RemoteHash,
			ActorEmail: config.ActorEmail,
		},
	); err != nil {
		return fmt.Errorf("report workspace docs hash: %w", err)
	}
	state.Reported = true
	if err := store.Save(*state); err != nil {
		return fmt.Errorf("persist workspace docs hash report: %w", err)
	}
	return nil
}

func (e *pullCommitEngine) writeRemoteHash(
	workDir string,
	config *client.WorkspaceConfig,
	remoteHash docs.CommitHash,
) error {
	hashPath := filepath.Join(workDir, config.DocsHashFile)
	if err := e.docsHashStore.Write(hashPath, remoteHash); err != nil {
		return fmt.Errorf("update docs hash after sync commit: %w", err)
	}
	return nil
}

func (e *pullCommitEngine) finishManual(ctx context.Context, workDir string, config *client.WorkspaceConfig) error {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return err
	}
	state, exists, err := store.Load()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("no pull-commit transaction exists")
	}
	if state.Phase != fs.PullCommitPhaseSyncCommitted && state.Phase != fs.PullCommitPhasePrepared {
		return fmt.Errorf("pull-commit is not ready to finish: %s", state.Phase)
	}
	if err := e.prepare(ctx, workDir, config, store, &state); err != nil {
		return err
	}
	return store.Remove()
}

func (e *pullCommitEngine) abort(ctx context.Context, workDir string, config *client.WorkspaceConfig) error {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return err
	}
	state, exists, err := store.Load()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("no pull-commit transaction exists")
	}
	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return err
	}
	if head != state.OriginalHead {
		return errors.New("cannot abort after the docs sync commit changed HEAD")
	}
	indexSnapshot, err := store.ReadArtifact(fs.PullCommitOriginalIndexSnapshot)
	if err != nil {
		return err
	}
	workSnapshot, err := store.ReadArtifact(fs.PullCommitOriginalWorkSnapshot)
	if err != nil {
		return err
	}
	if err := e.workspaceSync.StageDocsSnapshot(ctx, workDir, config.DocsDir, indexSnapshot); err != nil {
		return err
	}
	if err := e.workspaceSync.ApplyWorktreeDocsSnapshot(workDir, config.DocsDir, workSnapshot); err != nil {
		return err
	}
	return store.Remove()
}

func (e *pullCommitEngine) clearAfterCommit(ctx context.Context, workDir string) error {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return err
	}
	state, exists, err := store.Load()
	if err != nil || !exists {
		return err
	}
	if state.Phase != fs.PullCommitPhasePrepared {
		return nil
	}
	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return err
	}
	preparedHead := pullCommitPreparedHead(state)
	if head == preparedHead {
		return nil
	}
	completed, err := e.workspaceSync.IsAncestor(ctx, workDir, preparedHead, head)
	if err != nil {
		return err
	}
	if !completed {
		return nil
	}
	return store.Remove()
}

func pullCommitPreparedHead(state fs.PullCommitState) string {
	if state.PreparedHead != "" {
		return state.PreparedHead
	}
	return state.SyncCommit
}

func newPullCommitTransactionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create pull-commit transaction id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func (e *pullCommitEngine) mergeSnapshots(
	ctx context.Context,
	docsDir string,
	base, index, work, remote []byte,
) ([]byte, []byte, []string, error) {
	tempDir, err := os.MkdirTemp("", "sanho-layered-merge-*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create merge directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	names := []struct {
		name     string
		snapshot []byte
	}{
		{"base", base},
		{"index", index},
		{"work", work},
		{"remote", remote},
	}
	for _, item := range names {
		root := filepath.Join(tempDir, item.name)
		if err := e.snapshotApplier.Apply(item.snapshot, root, docsDir); err != nil {
			return nil, nil, nil, fmt.Errorf("materialize %s snapshot: %w", item.name, err)
		}
	}

	merger := newPreCommitGitClientAdapter(e.gitClient)
	baseDir := filepath.Join(tempDir, "base", docsDir)
	remoteDir := filepath.Join(tempDir, "remote", docsDir)
	indexDir := filepath.Join(tempDir, "index", docsDir)
	workDir := filepath.Join(tempDir, "work", docsDir)
	_, indexConflicts, err := hook.MergeDirectories(ctx, merger, baseDir, indexDir, remoteDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("merge staged docs: %w", err)
	}
	_, workConflicts, err := hook.MergeDirectories(ctx, merger, baseDir, workDir, remoteDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("merge working docs: %w", err)
	}
	mergedIndex, err := e.snapshotBuilder.Build(indexDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("capture merged staged docs: %w", err)
	}
	mergedWork, err := e.snapshotBuilder.Build(workDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("capture merged working docs: %w", err)
	}

	conflictSet := make(map[string]struct{}, len(indexConflicts)+len(workConflicts))
	for _, path := range append(indexConflicts, workConflicts...) {
		conflictSet[path] = struct{}{}
	}
	conflicts := make([]string, 0, len(conflictSet))
	for path := range conflictSet {
		conflicts = append(conflicts, path)
	}
	sort.Strings(conflicts)
	return mergedIndex, mergedWork, conflicts, nil
}

type snapshotFile struct {
	data []byte
	mode os.FileMode
}

func (e *pullCommitEngine) overlayChangedSnapshotPaths(
	docsDir string,
	target, original, current []byte,
) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "sanho-index-overlay-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	roots, err := e.materializeSnapshots(tempDir, docsDir, map[string][]byte{
		"target":   target,
		"original": original,
		"current":  current,
	})
	if err != nil {
		return nil, err
	}
	originalFiles, err := readSnapshotFiles(roots["original"])
	if err != nil {
		return nil, err
	}
	currentFiles, err := readSnapshotFiles(roots["current"])
	if err != nil {
		return nil, err
	}
	paths := make(map[string]struct{}, len(originalFiles)+len(currentFiles))
	for path := range originalFiles {
		paths[path] = struct{}{}
	}
	for path := range currentFiles {
		paths[path] = struct{}{}
	}
	for path := range paths {
		if snapshotFilesEqual(originalFiles[path], currentFiles[path]) {
			continue
		}
		if err := applySnapshotFile(roots["target"], path, currentFiles[path]); err != nil {
			return nil, err
		}
	}
	return e.snapshotBuilder.Build(roots["target"])
}

func (e *pullCommitEngine) overlaySelectedSnapshotPaths(
	docsDir string,
	target, current []byte,
	paths []string,
) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "sanho-selected-overlay-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	roots, err := e.materializeSnapshots(tempDir, docsDir, map[string][]byte{
		"target":  target,
		"current": current,
	})
	if err != nil {
		return nil, err
	}
	currentFiles, err := readSnapshotFiles(roots["current"])
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		clean := filepath.Clean(path)
		if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
			len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
			return nil, fmt.Errorf("invalid snapshot path %q", path)
		}
		if err := applySnapshotFile(roots["target"], clean, currentFiles[clean]); err != nil {
			return nil, err
		}
	}
	return e.snapshotBuilder.Build(roots["target"])
}

func (e *pullCommitEngine) snapshotsEqual(docsDir string, left, right []byte) (bool, error) {
	tempDir, err := os.MkdirTemp("", "sanho-snapshot-compare-*")
	if err != nil {
		return false, err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	roots, err := e.materializeSnapshots(tempDir, docsDir, map[string][]byte{
		"left":  left,
		"right": right,
	})
	if err != nil {
		return false, err
	}
	leftFiles, err := readSnapshotFiles(roots["left"])
	if err != nil {
		return false, err
	}
	rightFiles, err := readSnapshotFiles(roots["right"])
	if err != nil {
		return false, err
	}
	if len(leftFiles) != len(rightFiles) {
		return false, nil
	}
	for path, leftFile := range leftFiles {
		if !snapshotFilesEqual(leftFile, rightFiles[path]) {
			return false, nil
		}
	}
	return true, nil
}

func (e *pullCommitEngine) materializeSnapshots(
	tempDir, docsDir string,
	snapshots map[string][]byte,
) (map[string]string, error) {
	roots := make(map[string]string, len(snapshots))
	for name, snapshot := range snapshots {
		root := filepath.Join(tempDir, name)
		if err := e.snapshotApplier.Apply(snapshot, root, docsDir); err != nil {
			return nil, fmt.Errorf("materialize %s snapshot: %w", name, err)
		}
		roots[name] = filepath.Join(root, docsDir)
	}
	return roots, nil
}

func readSnapshotFiles(root string) (map[string]snapshotFile, error) {
	files := make(map[string]snapshotFile)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = snapshotFile{data: data, mode: info.Mode().Perm()}
		return nil
	})
	return files, err
}

func snapshotFilesEqual(left, right snapshotFile) bool {
	return left.mode == right.mode && bytes.Equal(left.data, right.data)
}

func applySnapshotFile(root, path string, file snapshotFile) error {
	target := filepath.Join(root, path)
	if file.data == nil {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return os.WriteFile(target, file.data, file.mode)
}
