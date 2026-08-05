package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

const (
	provenanceHashOne = "1111111111111111111111111111111111111111"
	provenanceHashTwo = "2222222222222222222222222222222222222222"
)

type fakeDocsProvenanceHTTPClient struct {
	snapshot       docs.DocsSnapshot
	snapshotHash   docs.CommitHash
	snapshotErr    error
	statusRelation docs.CommitRelationStatus
	statusErr      error
}

func (f *fakeDocsProvenanceHTTPClient) DocsSnapshot(
	_ context.Context,
	_ docs.ProjectName,
	commit docs.CommitHash,
) (docs.DocsSnapshot, docs.CommitHash, error) {
	if f.snapshotErr != nil {
		return nil, "", f.snapshotErr
	}
	actual := f.snapshotHash
	if actual.IsZero() {
		actual = commit
	}
	return f.snapshot, actual, nil
}

func (f *fakeDocsProvenanceHTTPClient) GetProjectStatus(
	_ context.Context,
	_ docs.ProjectName,
	_ workspace.WorkspaceID,
	docsHash docs.CommitHash,
) (httpclient.ProjectStatusResponse, error) {
	if f.statusErr != nil {
		return httpclient.ProjectStatusResponse{}, f.statusErr
	}
	relation := f.statusRelation
	if relation == "" {
		relation = docs.CommitRelationSame
	}
	return httpclient.ProjectStatusResponse{
		ReferenceDocsHash: string(docsHash),
		DocsHead:          string(docsHash),
		ReferenceToHead: httpclient.CommitRelation{
			Status: relation,
		},
	}, nil
}

func TestDocsProvenanceVerifierAcceptsArbitraryTipAndNonDocsDescendants(t *testing.T) {
	repo := newDocsProvenanceRepo(t)
	writeDocsProvenanceFile(t, repo, "docs/guide.md", "canonical\n")
	docsCommit := commitDocsProvenanceRepo(t, repo, "publish docs\n\ndocs-version: "+provenanceHashOne)
	writeDocsProvenanceFile(t, repo, "app.txt", "application\n")
	validTip := commitDocsProvenanceRepo(t, repo, "application only")

	// Move checked-out HEAD to a different unmanaged docs tree. Verification
	// must still use the proposed OID rather than the current checkout.
	writeDocsProvenanceFile(t, repo, "docs/guide.md", "unmanaged\n")
	_ = commitDocsProvenanceRepo(t, repo, "unmanaged current HEAD")

	httpClient := &fakeDocsProvenanceHTTPClient{
		snapshot:       buildDocsProvenanceSnapshot(t, map[string]string{"guide.md": "canonical\n"}),
		statusRelation: docs.CommitRelationBehind,
	}
	result, err := newDocsProvenanceVerifier(httpClient).Verify(
		context.Background(),
		repo,
		validTip,
		docsProvenanceConfig(),
	)
	if err != nil || !result.Valid {
		t.Fatalf("Verify() result=%+v err=%v", result, err)
	}
	if result.Classification != docsProvenanceValid || result.AppCommit != docsCommit || result.DocsHash != provenanceHashOne {
		t.Fatalf("Verify() result=%+v", result)
	}
}

func TestDocsProvenanceVerifierRejectsMissingAndMalformedTrailers(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repo := newDocsProvenanceRepo(t)
		writeDocsProvenanceFile(t, repo, "docs/guide.md", "unmanaged\n")
		tip := commitDocsProvenanceRepo(t, repo, "unmanaged docs")
		result, err := newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{}).Verify(
			context.Background(), repo, tip, docsProvenanceConfig(),
		)
		if err != nil || result.Valid || result.Classification != docsProvenanceMissing {
			t.Fatalf("Verify() result=%+v err=%v", result, err)
		}
	})

	for _, test := range []struct {
		name    string
		message string
	}{
		{name: "abbreviated", message: "docs\n\ndocs-version: deadbeef"},
		{name: "uppercase", message: "docs\n\ndocs-version: 111111111111111111111111111111111111111A"},
		{name: "duplicate", message: "docs\n\ndocs-version: " + provenanceHashOne + "\ndocs-version: " + provenanceHashTwo},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newDocsProvenanceRepo(t)
			writeDocsProvenanceFile(t, repo, "docs/guide.md", "docs\n")
			tip := commitDocsProvenanceRepo(t, repo, test.message)
			result, err := newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{}).Verify(
				context.Background(), repo, tip, docsProvenanceConfig(),
			)
			if err != nil || result.Valid || result.Classification != docsProvenanceInvalidTrailer {
				t.Fatalf("Verify() result=%+v err=%v", result, err)
			}
		})
	}
}

func TestDocsProvenanceVerifierRejectsUnknownForgedNewestTrailer(t *testing.T) {
	repo := newDocsProvenanceRepo(t)
	writeDocsProvenanceFile(t, repo, "docs/guide.md", "canonical\n")
	_ = commitDocsProvenanceRepo(t, repo, "valid docs\n\ndocs-version: "+provenanceHashOne)
	writeDocsProvenanceFile(t, repo, "app.txt", "forged metadata\n")
	forgedCommit := commitDocsProvenanceRepo(t, repo, "forged\n\ndocs-version: "+provenanceHashTwo)

	result, err := newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{
		statusErr: httpclient.ErrUnknownDocsCommit,
	}).Verify(context.Background(), repo, forgedCommit, docsProvenanceConfig())
	if err != nil || result.Valid || result.Classification != docsProvenanceUnknownCanonical {
		t.Fatalf("Verify() result=%+v err=%v", result, err)
	}
	if result.AppCommit != forgedCommit || result.DocsHash != provenanceHashTwo {
		t.Fatalf("verifier fell back past forged governing commit: %+v", result)
	}
}

func TestDocsProvenanceVerifierRejectsUnreachableAndTreeMismatchedCanonicalCommit(t *testing.T) {
	repo := newDocsProvenanceRepo(t)
	writeDocsProvenanceFile(t, repo, "docs/guide.md", "application\n")
	tip := commitDocsProvenanceRepo(t, repo, "docs\n\ndocs-version: "+provenanceHashOne)

	t.Run("unreachable", func(t *testing.T) {
		result, err := newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{
			statusRelation: docs.CommitRelationDiverged,
		}).Verify(context.Background(), repo, tip, docsProvenanceConfig())
		if err != nil || result.Valid || result.Classification != docsProvenanceUnreachable {
			t.Fatalf("Verify() result=%+v err=%v", result, err)
		}
	})

	t.Run("tree mismatch", func(t *testing.T) {
		result, err := newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{
			snapshot: buildDocsProvenanceSnapshot(t, map[string]string{"guide.md": "different\n"}),
		}).Verify(context.Background(), repo, tip, docsProvenanceConfig())
		if err != nil || result.Valid || result.Classification != docsProvenanceSnapshotMismatch {
			t.Fatalf("Verify() result=%+v err=%v", result, err)
		}
	})
}

func TestDocsProvenanceVerifierPropagatesOperationalErrors(t *testing.T) {
	repo := newDocsProvenanceRepo(t)
	writeDocsProvenanceFile(t, repo, "docs/guide.md", "application\n")
	tip := commitDocsProvenanceRepo(t, repo, "docs\n\ndocs-version: "+provenanceHashOne)
	want := errors.New("daemon unavailable")
	_, err := newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{statusErr: want}).Verify(
		context.Background(), repo, tip, docsProvenanceConfig(),
	)
	if !errors.Is(err, want) {
		t.Fatalf("Verify() error=%v, want %v", err, want)
	}
}

func TestWorkspaceReconciliationValidatesCanonicalProvenanceBeforeHashFastPath(t *testing.T) {
	repo := newDocsProvenanceRepo(t)
	writeDocsProvenanceFile(t, repo, "docs/guide.md", "application\n")
	_ = commitDocsProvenanceRepo(t, repo, "docs\n\ndocs-version: "+provenanceHashOne)
	if err := os.WriteFile(filepath.Join(repo, ".sanho_docs_hash"), []byte(provenanceHashOne+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	config := docsProvenanceConfig()

	changed, err := reconcileWorkspaceDocsFromHEADWithVerifier(
		context.Background(),
		repo,
		config,
		workspaceMutationPermit{},
		newDocsProvenanceVerifier(&fakeDocsProvenanceHTTPClient{statusErr: httpclient.ErrUnknownDocsCommit}),
	)
	if err == nil || changed || !strings.Contains(err.Error(), string(docsProvenanceUnknownCanonical)) {
		t.Fatalf("reconcile changed=%t err=%v, want unknown canonical rejection", changed, err)
	}
	data, readErr := os.ReadFile(filepath.Join(repo, ".sanho_docs_hash"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.TrimSpace(string(data)) != provenanceHashOne {
		t.Fatalf("rejected reconciliation changed docs hash: %q", data)
	}
}

func docsProvenanceConfig() *client.WorkspaceConfig {
	return &client.WorkspaceConfig{
		Project:     "project",
		WorkspaceID: "workspace",
		DocsDir:     "docs",
	}
}

func newDocsProvenanceRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runDocsProvenanceGit(t, repo, "init")
	runDocsProvenanceGit(t, repo, "config", "user.email", "test@example.com")
	runDocsProvenanceGit(t, repo, "config", "user.name", "Test User")
	return repo
}

func writeDocsProvenanceFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func commitDocsProvenanceRepo(t *testing.T, repo, message string) string {
	t.Helper()
	runDocsProvenanceGit(t, repo, "add", ".")
	runDocsProvenanceGit(t, repo, "commit", "-m", message)
	return strings.TrimSpace(runDocsProvenanceGit(t, repo, "rev-parse", "HEAD"))
}

func runDocsProvenanceGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

func buildDocsProvenanceSnapshot(t *testing.T, files map[string]string) docs.DocsSnapshot {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		writeDocsProvenanceFile(t, dir, name, content)
	}
	snapshot, err := fs.NewSnapshotBuilder().Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	return docs.DocsSnapshot(snapshot)
}
