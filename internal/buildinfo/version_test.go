package buildinfo

import "testing"

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name          string
		injected      string
		moduleVersion string
		want          string
	}{
		{name: "injected version wins", injected: "v1.2.3", moduleVersion: "v1.0.0", want: "v1.2.3"},
		{name: "module version supports go install", injected: "dev", moduleVersion: "v1.2.3", want: "v1.2.3"},
		{name: "pseudo version is preserved", moduleVersion: "v0.0.0-20260729000000-deadbeef", want: "v0.0.0-20260729000000-deadbeef"},
		{name: "local build is development", injected: "dev", moduleVersion: "(devel)", want: DevelopmentVersion},
		{name: "empty values are development", want: DevelopmentVersion},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveVersion(test.injected, test.moduleVersion); got != test.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", test.injected, test.moduleVersion, got, test.want)
			}
		})
	}
}
