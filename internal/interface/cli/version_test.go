package cli

import (
	"testing"
)

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		commit    string
		buildDate string
		want      string
	}{
		{
			name:      "all values provided",
			version:   "1.0.0",
			commit:    "abc1234",
			buildDate: "2024-01-15T10:00:00Z",
			want:      "kkachi version 1.0.0 (commit: abc1234, built: 2024-01-15T10:00:00Z)",
		},
		{
			name:      "empty version defaults to dev",
			version:   "",
			commit:    "abc1234",
			buildDate: "2024-01-15T10:00:00Z",
			want:      "kkachi version dev (commit: abc1234, built: 2024-01-15T10:00:00Z)",
		},
		{
			name:      "empty commit defaults to unknown",
			version:   "1.0.0",
			commit:    "",
			buildDate: "2024-01-15T10:00:00Z",
			want:      "kkachi version 1.0.0 (commit: unknown, built: 2024-01-15T10:00:00Z)",
		},
		{
			name:      "empty buildDate defaults to unknown",
			version:   "1.0.0",
			commit:    "abc1234",
			buildDate: "",
			want:      "kkachi version 1.0.0 (commit: abc1234, built: unknown)",
		},
		{
			name:      "all empty defaults",
			version:   "",
			commit:    "",
			buildDate: "",
			want:      "kkachi version dev (commit: unknown, built: unknown)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatVersion(tt.version, tt.commit, tt.buildDate)
			if got != tt.want {
				t.Errorf("FormatVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
