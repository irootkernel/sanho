package buildinfo

import "testing"

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name          string
		configured    string
		moduleVersion string
		want          string
	}{
		{name: "configured version wins", configured: "v1.2.3", moduleVersion: "v1.0.0", want: "v1.2.3"},
		{name: "module version supports go install", configured: "dev", moduleVersion: "v1.2.3", want: "v1.2.3"},
		{name: "pseudo version is preserved", moduleVersion: "v0.0.0-20260729000000-deadbeef", want: "v0.0.0-20260729000000-deadbeef"},
		{name: "local build is development", configured: "dev", moduleVersion: "(devel)", want: DevelopmentVersion},
		{name: "empty values are development", want: DevelopmentVersion},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveVersion(test.configured, test.moduleVersion); got != test.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", test.configured, test.moduleVersion, got, test.want)
			}
		})
	}
}
