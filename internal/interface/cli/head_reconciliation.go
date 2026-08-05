package cli

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

type headReconciliationClassification string

const (
	headReconciliationReconciled headReconciliationClassification = "reconciled"
	headReconciliationPending    headReconciliationClassification = "pending"
	headReconciliationBlocked    headReconciliationClassification = "blocked"
	headReconciliationInvalid    headReconciliationClassification = "invalid"
	headReconciliationUnknown    headReconciliationClassification = "unknown"
)

type headReconciliationAssessment struct {
	Pending        bool
	Classification headReconciliationClassification
	AppCommit      string
	DocsHash       docs.CommitHash
	DocsRelation   httpclient.CommitRelation
	Reason         string
}

func assessHeadReconciliation(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	localHash docs.CommitHash,
	operation infraGit.GitOperation,
	httpClient docsProvenanceHTTPClient,
) headReconciliationAssessment {
	if operation.Active {
		return headReconciliationAssessment{
			Classification: headReconciliationBlocked,
			Reason:         "Git operation metadata must clear before local reconciliation",
		}
	}
	provenance, err := newDocsProvenanceVerifier(httpClient).Verify(ctx, workDir, "HEAD", config)
	if err != nil {
		return headReconciliationAssessment{
			Classification: headReconciliationUnknown,
			Reason:         err.Error(),
		}
	}
	assessment := headReconciliationAssessment{
		AppCommit:    provenance.AppCommit,
		DocsHash:     provenance.DocsHash,
		DocsRelation: provenance.DocsRelation,
		Reason:       provenance.Reason,
	}
	if !provenance.Valid {
		assessment.Classification = headReconciliationInvalid
		return assessment
	}
	assessment.Pending = provenance.DocsHash != localHash
	if assessment.Pending {
		assessment.Classification = headReconciliationPending
	} else {
		assessment.Classification = headReconciliationReconciled
	}
	return assessment
}
