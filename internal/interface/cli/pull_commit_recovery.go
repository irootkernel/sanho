package cli

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
)

type pullCommitClassification string

const (
	pullCommitPending            pullCommitClassification = "pending"
	pullCommitCompleted          pullCommitClassification = "completed"
	pullCommitRewritten          pullCommitClassification = "rewritten"
	pullCommitRecoverableRewrite pullCommitClassification = "recoverable_rewrite"
	pullCommitAmbiguous          pullCommitClassification = "ambiguous"
	pullCommitCorrupt            pullCommitClassification = "corrupt"
)

type pullCommitAssessment struct {
	State          fs.PullCommitState
	Exists         bool
	Classification pullCommitClassification
	Reason         string
	NextCommand    string
	Head           string
}

func (e *pullCommitEngine) assessTransaction(
	ctx context.Context,
	workDir, docsDir string,
) (pullCommitAssessment, error) {
	isRepository, err := e.workspaceSync.IsRepository(ctx, workDir)
	if err != nil || !isRepository {
		return pullCommitAssessment{}, err
	}
	store, err := e.store(ctx, workDir)
	if err != nil {
		return pullCommitAssessment{}, err
	}
	state, exists, err := store.Load()
	if err != nil || !exists {
		return pullCommitAssessment{Exists: exists}, err
	}
	assessment := pullCommitAssessment{State: state, Exists: true}
	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		assessment.Classification = pullCommitCorrupt
		assessment.Reason = err.Error()
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	assessment.Head = head
	if state.Phase == fs.PullCommitPhaseCompleted {
		assessment.Classification = pullCommitCompleted
		assessment.Reason = "completion was recorded before cleanup was interrupted"
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	if state.Phase != fs.PullCommitPhasePrepared {
		assessment.Classification = pullCommitPending
		assessment.Reason = fmt.Sprintf("transaction is in %s phase", state.Phase)
		if state.Phase == fs.PullCommitPhaseConflict {
			assessment.NextCommand = "sanho pull-commit --continue"
		} else {
			assessment.NextCommand = "sanho pull-commit --abort"
		}
		return assessment, nil
	}

	preparedHead := pullCommitPreparedHead(state)
	if preparedHead == "" || state.SyncCommit == "" {
		assessment.Classification = pullCommitCorrupt
		assessment.Reason = "prepared transaction is missing its commit anchors"
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	if head == preparedHead {
		assessment.Classification = pullCommitPending
		assessment.Reason = "prepared changes have not been committed yet"
		assessment.NextCommand = "repeat the original git commit command"
		return assessment, nil
	}
	descendant, err := e.workspaceSync.IsAncestor(ctx, workDir, preparedHead, head)
	if err != nil {
		assessment.Classification = pullCommitCorrupt
		assessment.Reason = err.Error()
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	if descendant {
		assessment.Classification = pullCommitCompleted
		assessment.Reason = "current HEAD descends from the prepared commit"
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	containsSync, err := e.workspaceSync.IsAncestor(ctx, workDir, state.SyncCommit, head)
	if err != nil {
		assessment.Classification = pullCommitCorrupt
		assessment.Reason = err.Error()
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	if !containsSync && state.SyncCommit != preparedHead {
		assessment.Classification = pullCommitAmbiguous
		assessment.Reason = "current HEAD does not contain the recorded docs sync commit"
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	if state.PreparedTree != "" {
		tree, err := e.workspaceSync.CommitTree(ctx, workDir, head)
		if err != nil {
			assessment.Classification = pullCommitCorrupt
			assessment.Reason = err.Error()
			assessment.NextCommand = "sanho pull-commit --recover"
			return assessment, nil
		}
		if tree == state.PreparedTree {
			assessment.Classification = pullCommitRewritten
			assessment.Reason = "current HEAD contains the prepared index tree after a history rewrite"
			assessment.NextCommand = "sanho pull-commit --recover"
			return assessment, nil
		}
	}
	preparedParents, preparedErr := e.workspaceSync.CommitParents(ctx, workDir, preparedHead)
	headParents, headErr := e.workspaceSync.CommitParents(ctx, workDir, head)
	if preparedErr == nil && headErr == nil && slices.Equal(preparedParents, headParents) {
		if state.Version <= 2 {
			classification, reason := e.assessLegacyRewriteDocs(ctx, workDir, docsDir, store, head)
			if classification != pullCommitRecoverableRewrite {
				assessment.Classification = classification
				assessment.Reason = reason
				assessment.NextCommand = "sanho pull-commit --recover"
				return assessment, nil
			}
		}
		assessment.Classification = pullCommitRecoverableRewrite
		assessment.Reason = "current HEAD is a sibling rewrite with the recorded docs snapshot"
		assessment.NextCommand = "sanho pull-commit --recover"
		return assessment, nil
	}
	assessment.Classification = pullCommitAmbiguous
	assessment.Reason = "HEAD changed without a provable commit or rewrite relationship"
	assessment.NextCommand = "sanho pull-commit --recover"
	return assessment, nil
}

func (e *pullCommitEngine) assessLegacyRewriteDocs(
	ctx context.Context,
	workDir, docsDir string,
	store *fs.PullCommitStore,
	head string,
) (pullCommitClassification, string) {
	if docsDir == "" {
		return pullCommitCorrupt, "legacy rewrite validation requires the workspace docs directory"
	}
	expected, err := store.ReadArtifact(fs.PullCommitMergedIndexSnapshot)
	if err != nil {
		return pullCommitCorrupt, fmt.Sprintf("legacy merged index snapshot is unavailable: %v", err)
	}
	committed, err := e.workspaceSync.ArchiveCommitDocs(ctx, workDir, head, docsDir)
	if err != nil {
		return pullCommitCorrupt, fmt.Sprintf("current commit docs snapshot is unavailable: %v", err)
	}
	equal, err := fs.SnapshotsSemanticallyEqual(expected, "", committed, docsDir)
	if err != nil {
		return pullCommitCorrupt, fmt.Sprintf("legacy merged index snapshot is invalid: %v", err)
	}
	if !equal {
		return pullCommitAmbiguous, "current HEAD docs differ from the legacy merged index snapshot"
	}
	return pullCommitRecoverableRewrite, ""
}

func (e *pullCommitEngine) recover(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
) (pullCommitAssessment, error) {
	assessment, err := e.assessTransaction(ctx, workDir, config.DocsDir)
	if err != nil {
		return assessment, err
	}
	if !assessment.Exists {
		assessment.Classification = pullCommitCompleted
		assessment.Reason = "no active pull-commit transaction exists"
		return assessment, nil
	}
	store, err := e.store(ctx, workDir)
	if err != nil {
		return assessment, err
	}
	state := assessment.State
	if state.TransactionID == "" {
		state.TransactionID, err = newPullCommitTransactionID()
		if err != nil {
			return assessment, err
		}
		state.Version = 3
		if err := store.Save(state); err != nil {
			return assessment, err
		}
		if err := e.runRecoveryStep("transaction-id-saved"); err != nil {
			return assessment, err
		}
	}
	assessment.State = state
	if state.Recovery == nil {
		checkpoint, err := e.workspaceSync.CreateRecoveryCheckpoint(ctx, workDir, state.TransactionID)
		if err != nil {
			return assessment, err
		}
		if err := e.runRecoveryStep("checkpoint-created"); err != nil {
			return assessment, err
		}
		state.Recovery = &fs.PullCommitRecovery{
			HeadRef:     checkpoint.HeadRef,
			IndexRef:    checkpoint.IndexRef,
			WorktreeRef: checkpoint.WorktreeRef,
			CreatedAt:   time.Now(),
		}
		if err := store.Save(state); err != nil {
			return assessment, err
		}
		if err := e.runRecoveryStep("checkpoint-recorded"); err != nil {
			return assessment, err
		}
	}

	switch assessment.Classification {
	case pullCommitCompleted, pullCommitRewritten, pullCommitRecoverableRewrite:
		state.Phase = fs.PullCommitPhaseCompleted
		state.CompletionHead = assessment.Head
		state.CompletionReason = "manual-recovery-" + string(assessment.Classification)
		if err := store.Save(state); err != nil {
			return assessment, err
		}
		if err := e.runRecoveryStep("completion-recorded"); err != nil {
			return assessment, err
		}
		return assessment, store.Remove()
	case pullCommitPending:
		return assessment, fmt.Errorf("transaction is still pending: %s; next: %s", assessment.Reason, assessment.NextCommand)
	case pullCommitAmbiguous, pullCommitCorrupt:
		return assessment, fmt.Errorf(
			"transaction is %s: %s; recovery refs were preserved under refs/sanho/recovery/%s",
			assessment.Classification,
			assessment.Reason,
			state.TransactionID,
		)
	default:
		return assessment, fmt.Errorf("unknown pull-commit classification %q", assessment.Classification)
	}
}

func (e *pullCommitEngine) completeTransaction(
	ctx context.Context,
	workDir string,
	assessment pullCommitAssessment,
	reason string,
) error {
	store, err := e.store(ctx, workDir)
	if err != nil {
		return err
	}
	state := assessment.State
	state.Version = 3
	state.Phase = fs.PullCommitPhaseCompleted
	state.CompletionHead = assessment.Head
	state.CompletionReason = reason
	if err := store.Save(state); err != nil {
		return err
	}
	if err := e.runRecoveryStep("completion-recorded"); err != nil {
		return err
	}
	return store.Remove()
}

func (e *pullCommitEngine) runRecoveryStep(step string) error {
	if e.recoveryStep == nil {
		return nil
	}
	return e.recoveryStep(step)
}
