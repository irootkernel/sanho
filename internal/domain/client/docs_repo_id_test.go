package client

import (
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

func TestExtractDocsRepoID(t *testing.T) {
	tests := []struct {
		name   string
		gitURL string
		want   docs.DocsRepoID
	}{
		{
			name:   "SSH URL with .git suffix",
			gitURL: "git@github.com:SeventeenthEarth/sudal_docs.git",
			want:   "sudal_docs",
		},
		{
			name:   "SSH URL without .git suffix",
			gitURL: "git@github.com:SeventeenthEarth/sudal_docs",
			want:   "sudal_docs",
		},
		{
			name:   "HTTPS URL with .git suffix",
			gitURL: "https://github.com/SeventeenthEarth/sudal_docs.git",
			want:   "sudal_docs",
		},
		{
			name:   "HTTPS URL without .git suffix",
			gitURL: "https://github.com/SeventeenthEarth/sudal_docs",
			want:   "sudal_docs",
		},
		{
			name:   "GitLab SSH URL",
			gitURL: "git@gitlab.com:company/project-docs.git",
			want:   "project-docs",
		},
		{
			name:   "Bitbucket SSH URL",
			gitURL: "git@bitbucket.org:team/docs-repo.git",
			want:   "docs-repo",
		},
		{
			name:   "empty string",
			gitURL: "",
			want:   "",
		},
		{
			name:   "repo with underscores and dashes",
			gitURL: "git@github.com:org/my_docs-repo.git",
			want:   "my_docs-repo",
		},
		{
			name:   "deep path SSH URL",
			gitURL: "git@github.com:org/group/subgroup/repo.git",
			want:   "repo",
		},
		{
			name:   "deep path HTTPS URL",
			gitURL: "https://github.com/org/group/subgroup/repo.git",
			want:   "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDocsRepoID(tt.gitURL)
			if got != tt.want {
				t.Errorf("ExtractDocsRepoID(%q) = %q, want %q", tt.gitURL, got, tt.want)
			}
		})
	}
}
