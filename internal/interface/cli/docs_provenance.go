package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

type docsProvenanceClassification string

const (
	docsProvenanceValid            docsProvenanceClassification = "valid"
	docsProvenanceMissing          docsProvenanceClassification = "missing_docs_version"
	docsProvenanceInvalidTrailer   docsProvenanceClassification = "invalid_docs_version"
	docsProvenanceUnknownCanonical docsProvenanceClassification = "unknown_docs_version"
	docsProvenanceUnreachable      docsProvenanceClassification = "unreachable_docs_version"
	docsProvenanceSnapshotMismatch docsProvenanceClassification = "docs_tree_mismatch"
)

type docsProvenanceResult struct {
	Valid          bool
	Classification docsProvenanceClassification
	ProposedTip    string
	AppCommit      string
	DocsHash       docs.CommitHash
	DocsRelation   httpclient.CommitRelation
	Reason         string
}

type docsProvenanceHTTPClient interface {
	DocsSnapshot(
		ctx context.Context,
		project docs.ProjectName,
		commit docs.CommitHash,
	) (docs.DocsSnapshot, docs.CommitHash, error)
	GetProjectStatus(
		ctx context.Context,
		project docs.ProjectName,
		workspaceID workspace.WorkspaceID,
		docsHash docs.CommitHash,
	) (httpclient.ProjectStatusResponse, error)
}

type docsProvenanceVerifier struct {
	gitClient     *infraGit.Client
	workspaceSync *infraGit.WorkspaceSync
	httpClient    docsProvenanceHTTPClient
}

func newDocsProvenanceVerifier(httpClient docsProvenanceHTTPClient) *docsProvenanceVerifier {
	return &docsProvenanceVerifier{
		gitClient:     infraGit.NewClient(),
		workspaceSync: infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier()),
		httpClient:    httpClient,
	}
}

var fullObjectIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func (v *docsProvenanceVerifier) Verify(
	ctx context.Context,
	workDir, proposedTip string,
	config *client.WorkspaceConfig,
) (docsProvenanceResult, error) {
	config.ApplyDefaults()
	result := docsProvenanceResult{ProposedTip: proposedTip}
	candidate, found, err := v.gitClient.ResolveDocsVersionCandidate(
		ctx,
		workDir,
		proposedTip,
		config.DocsDir,
	)
	if err != nil {
		return result, err
	}
	if !found {
		result.Classification = docsProvenanceMissing
		result.Reason = "no reachable docs-version commit matches the proposed docs tree"
		return result, nil
	}
	result.AppCommit = candidate.AppCommit
	if len(candidate.TrailerValues) != 1 || !fullObjectIDPattern.MatchString(candidate.TrailerValues[0]) {
		result.Classification = docsProvenanceInvalidTrailer
		result.Reason = "the governing application commit must contain exactly one full lowercase docs-version object ID"
		return result, nil
	}
	result.DocsHash = docs.CommitHash(candidate.TrailerValues[0])

	projectStatus, err := v.httpClient.GetProjectStatus(
		ctx,
		config.Project,
		config.WorkspaceID,
		result.DocsHash,
	)
	if err != nil {
		if errors.Is(err, httpclient.ErrUnknownDocsCommit) {
			result.Classification = docsProvenanceUnknownCanonical
			result.Reason = "docs-version does not identify a canonical docs commit"
			return result, nil
		}
		return result, fmt.Errorf("validate canonical docs commit: %w", err)
	}
	if projectStatus.ReferenceDocsHash != string(result.DocsHash) ||
		(projectStatus.ReferenceToHead.Status != docs.CommitRelationSame &&
			projectStatus.ReferenceToHead.Status != docs.CommitRelationBehind) {
		result.Classification = docsProvenanceUnreachable
		result.Reason = "docs-version is not reachable from the canonical docs HEAD"
		return result, nil
	}
	result.DocsRelation = projectStatus.ReferenceToHead

	canonicalSnapshot, actualHash, err := v.httpClient.DocsSnapshot(ctx, config.Project, result.DocsHash)
	if err != nil {
		if errors.Is(err, httpclient.ErrUnknownDocsCommit) {
			result.Classification = docsProvenanceUnknownCanonical
			result.Reason = "docs-version does not identify a canonical docs snapshot"
			return result, nil
		}
		return result, fmt.Errorf("load canonical docs snapshot: %w", err)
	}
	if actualHash != result.DocsHash {
		result.Classification = docsProvenanceUnknownCanonical
		result.Reason = "canonical docs snapshot resolved to a different commit"
		return result, nil
	}
	applicationSnapshot, err := v.workspaceSync.ArchiveCommitDocs(
		ctx,
		workDir,
		candidate.AppCommit,
		config.DocsDir,
	)
	if err != nil {
		return result, err
	}
	equal, err := fs.SnapshotsSemanticallyEqual(
		canonicalSnapshot,
		"",
		applicationSnapshot,
		config.DocsDir,
	)
	if err != nil {
		return result, fmt.Errorf("compare canonical and application docs snapshots: %w", err)
	}
	if !equal {
		result.Classification = docsProvenanceSnapshotMismatch
		result.Reason = "canonical docs snapshot does not match the proposed application docs tree"
		return result, nil
	}

	result.Valid = true
	result.Classification = docsProvenanceValid
	result.Reason = "proposed application docs have valid canonical provenance"
	return result, nil
}
