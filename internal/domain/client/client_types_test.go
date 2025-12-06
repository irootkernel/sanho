package client

import "testing"

func TestDocsStatus_IsZero(t *testing.T) {
	tests := []struct {
		name   string
		status DocsStatus
		want   bool
	}{
		{"empty string is zero", DocsStatus(""), true},
		{"unknown is not zero", DocsStatusUnknown, false},
		{"up_to_date is not zero", DocsStatusUpToDate, false},
		{"outdated is not zero", DocsStatusOutdated, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsZero(); got != tt.want {
				t.Errorf("DocsStatus.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDocsStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status DocsStatus
		want   string
	}{
		{"unknown", DocsStatusUnknown, "unknown"},
		{"up_to_date", DocsStatusUpToDate, "up_to_date"},
		{"outdated", DocsStatusOutdated, "outdated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("DocsStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDocsStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status DocsStatus
		want   bool
	}{
		{"unknown is valid", DocsStatusUnknown, true},
		{"up_to_date is valid", DocsStatusUpToDate, true},
		{"outdated is valid", DocsStatusOutdated, true},
		{"empty is invalid", DocsStatus(""), false},
		{"arbitrary string is invalid", DocsStatus("random"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("DocsStatus.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkspaceConfig_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name         string
		config       WorkspaceConfig
		wantDocsDir  string
		wantHashFile string
		wantFixFile  string
	}{
		{
			name:         "empty config gets defaults",
			config:       WorkspaceConfig{},
			wantDocsDir:  DefaultDocsDir,
			wantHashFile: DefaultDocsHashFile,
			wantFixFile:  DefaultPendingFixFile,
		},
		{
			name: "preserves existing values",
			config: WorkspaceConfig{
				DocsDir:        "custom_docs",
				DocsHashFile:   ".custom_hash",
				PendingFixFile: ".custom_fix",
			},
			wantDocsDir:  "custom_docs",
			wantHashFile: ".custom_hash",
			wantFixFile:  ".custom_fix",
		},
		{
			name: "partial defaults",
			config: WorkspaceConfig{
				DocsDir: "my_docs",
			},
			wantDocsDir:  "my_docs",
			wantHashFile: DefaultDocsHashFile,
			wantFixFile:  DefaultPendingFixFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config
			cfg.ApplyDefaults()

			if cfg.DocsDir != tt.wantDocsDir {
				t.Errorf("DocsDir = %v, want %v", cfg.DocsDir, tt.wantDocsDir)
			}
			if cfg.DocsHashFile != tt.wantHashFile {
				t.Errorf("DocsHashFile = %v, want %v", cfg.DocsHashFile, tt.wantHashFile)
			}
			if cfg.PendingFixFile != tt.wantFixFile {
				t.Errorf("PendingFixFile = %v, want %v", cfg.PendingFixFile, tt.wantFixFile)
			}
		})
	}
}
