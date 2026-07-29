package docs_test

import (
	"context"
	"errors"
	"testing"

	domain "github.com/irootkernel/sanho/internal/domain/docs"
	usecase "github.com/irootkernel/sanho/internal/usecase/docs"
)

type mockSnapshotRepo struct {
	head        domain.CommitHash
	headErr     error
	snapshot    domain.DocsSnapshot
	snapshotErr error
}

func (m *mockSnapshotRepo) GetHead(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error) {
	return m.head, m.headErr
}

func (m *mockSnapshotRepo) GetSnapshot(ctx context.Context, project domain.ProjectName, commit domain.CommitHash) (domain.DocsSnapshot, domain.CommitHash, error) {
	if m.snapshotErr != nil {
		return nil, "", m.snapshotErr
	}
	// Echo back commit by default.
	return m.snapshot, commit, nil
}

func TestGetDocsSnapshotUseCase_Execute(t *testing.T) {
	tests := []struct {
		name       string
		repo       *mockSnapshotRepo
		project    domain.ProjectName
		commit     domain.CommitHash
		wantSnap   domain.DocsSnapshot
		wantCommit domain.CommitHash
		wantErr    bool
	}{
		{
			name: "Success - Explicit Commit",
			repo: &mockSnapshotRepo{
				snapshot: []byte("data"),
			},
			project:    "proj1",
			commit:     "abc",
			wantSnap:   []byte("data"),
			wantCommit: "abc",
			wantErr:    false,
		},
		{
			name: "Success - HEAD fallback",
			repo: &mockSnapshotRepo{
				head:     "head123",
				snapshot: []byte("head_data"),
			},
			project:    "proj1",
			commit:     "",
			wantSnap:   []byte("head_data"),
			wantCommit: "head123",
			wantErr:    false,
		},
		{
			name: "Error - GetHead failed",
			repo: &mockSnapshotRepo{
				headErr: errors.New("head failed"),
			},
			project: "proj1",
			commit:  "",
			wantErr: true,
		},
		{
			name: "Error - GetSnapshot failed",
			repo: &mockSnapshotRepo{
				snapshotErr: errors.New("snapshot failed"),
			},
			project: "proj1",
			commit:  "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := usecase.NewGetDocsSnapshotUseCase(tt.repo)
			gotSnap, gotCommit, err := u.Execute(context.Background(), tt.project, tt.commit)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if string(gotSnap) != string(tt.wantSnap) {
					t.Errorf("Execute() snapshot = %v, want %v", string(gotSnap), string(tt.wantSnap))
				}
				if gotCommit != tt.wantCommit {
					t.Errorf("Execute() commit = %v, want %v", gotCommit, tt.wantCommit)
				}
			}
		})
	}
}
