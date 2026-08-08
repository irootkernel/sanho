package cli

import (
	"testing"

	"github.com/irootkernel/sanho/internal/infra/wsstate"
)

func TestIsManagedWorkspaceConfigAcceptsOnlyCompleteV1OrV2(t *testing.T) {
	tests := []struct {
		name   string
		config wsstate.Config
		want   bool
	}{
		{name: "v1", config: wsstate.Config{SchemaVersion: 1, Project: "product"}, want: true},
		{name: "v2", config: wsstate.Config{SchemaVersion: 2, Project: "product", DocsRepoURL: "repo"}, want: true},
		{name: "v1 missing project", config: wsstate.Config{SchemaVersion: 1}, want: false},
		{name: "v2 missing repository", config: wsstate.Config{SchemaVersion: 2, Project: "product"}, want: false},
		{name: "unknown schema", config: wsstate.Config{SchemaVersion: 3, Project: "product", DocsRepoURL: "repo"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isManagedWorkspaceConfig(test.config); got != test.want {
				t.Fatalf("isManagedWorkspaceConfig() = %t, want %t", got, test.want)
			}
		})
	}
}
