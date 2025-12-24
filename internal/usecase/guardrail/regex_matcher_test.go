package guardrail

import (
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
)

func TestRegexMatcher_Validate(t *testing.T) {
	rules := []config.SecurityRule{
		{Pattern: `rm -rf .*`, Reason: "Recursive deletion is blocked"},
		{Pattern: `mkfs.*`, Reason: "Filesystem creation is blocked"},
	}

	matcher, err := NewRegexMatcher(rules)
	if err != nil {
		t.Fatalf("failed to create matcher: %v", err)
	}

	tests := []struct {
		cmd      string
		expected bool
		reason   string
	}{
		{"ls -al", false, ""},
		{"rm -rf /", true, "Recursive deletion is blocked"},
		{"rm -rf .", true, "Recursive deletion is blocked"},
		{"mkfs.ext4 /dev/sda1", true, "Filesystem creation is blocked"},
		{"echo hello", false, ""},
	}

	for _, tt := range tests {
		result := matcher.Validate(tt.cmd)
		if result.Blocked != tt.expected {
			t.Errorf("Validate(%q) Blocked = %v, expected %v", tt.cmd, result.Blocked, tt.expected)
		}
		if result.Reason != tt.reason {
			t.Errorf("Validate(%q) Reason = %q, expected %q", tt.cmd, result.Reason, tt.reason)
		}
	}
}

func TestRegexMatcher_InvalidRegex(t *testing.T) {
	rules := []config.SecurityRule{
		{Pattern: `[invalid`, Reason: "Bad regex"},
	}

	_, err := NewRegexMatcher(rules)
	if err == nil {
		t.Error("expected error for invalid regex, got nil")
	}
}
