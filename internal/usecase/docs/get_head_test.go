package docs_test

import (
	"context"
	"errors"
	"testing"

	domain "github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	usecase "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

type mockDocsReadRepository struct {
	head domain.CommitHash
	err  error
}

func (m *mockDocsReadRepository) GetHead(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error) {
	return m.head, m.err
}

func (m *mockDocsReadRepository) GetSnapshot(ctx context.Context, project domain.ProjectName, commit domain.CommitHash) (domain.DocsSnapshot, domain.CommitHash, error) {
	return nil, "", errors.New("not implemented")
}

func TestGetDocsHeadUseCase_Execute(t *testing.T) {
	tests := []struct {
		name    string
		repo    *mockDocsReadRepository
		project domain.ProjectName
		want    domain.CommitHash
		wantErr bool
	}{
		{
			name:    "Success",
			repo:    &mockDocsReadRepository{head: "abcdef", err: nil},
			project: "test-project",
			want:    "abcdef",
			wantErr: false,
		},
		{
			name:    "Error",
			repo:    &mockDocsReadRepository{head: "", err: errors.New("repo error")},
			project: "test-project",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := usecase.NewGetDocsHeadUseCase(tt.repo)
			got, err := u.Execute(context.Background(), tt.project)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %v, want %v", got, tt.want)
			}
		})
	}
}
