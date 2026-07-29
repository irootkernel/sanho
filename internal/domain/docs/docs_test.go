package docs_test

import (
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

func TestCommitHash_IsZero(t *testing.T) {
	tests := []struct {
		name string
		h    docs.CommitHash
		want bool
	}{
		{"Empty", "", true},
		{"NonEmpty", "abcdef", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.h.IsZero(); got != tt.want {
				t.Errorf("CommitHash.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
