package cli

import (
	"bytes"
	"testing"
)

func TestVersionTextIsCompact(t *testing.T) {
	previousVerbose := verbose
	verbose = false
	t.Cleanup(func() { verbose = previousVerbose })
	cmd := newVersionCmd(BuildInfo{Version: "v0.2.6"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}

	const want = "sanho v0.2.6\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionJSONIsCompact(t *testing.T) {
	previousVerbose := verbose
	verbose = false
	t.Cleanup(func() { verbose = previousVerbose })
	cmd := newVersionCmd(BuildInfo{Version: "v0.2.6"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version --json: %v", err)
	}

	const want = "{\"name\":\"sanho\",\"version\":\"v0.2.6\"}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version --json output = %q, want %q", got, want)
	}
}

func TestVersionVerboseJSONIncludesGitSHA(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(
		BuildInfo{Version: "v0.2.8-dev.0123456789ab", GitSHA: "0123456789abcdef0123456789abcdef01234567"},
		[]string{"version", "--verbose", "--json"},
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf("version --verbose --json exit code = %d, stderr = %q", code, stderr.String())
	}
	const want = "{\"name\":\"sanho\",\"version\":\"v0.2.8-dev.0123456789ab\",\"git_sha\":\"0123456789abcdef0123456789abcdef01234567\"}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version --verbose --json output = %q, want %q", got, want)
	}
}

func TestVersionVerboseJSONUsesNullForUnknownGitSHA(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(BuildInfo{Version: "v0.2.8"}, []string{"version", "--verbose", "--json"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("version --verbose --json exit code = %d, stderr = %q", code, stderr.String())
	}
	const want = "{\"name\":\"sanho\",\"version\":\"v0.2.8\",\"git_sha\":null}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version --verbose --json output = %q, want %q", got, want)
	}
}

func TestVersionVerboseTextNamesUnknownGitSHA(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(BuildInfo{Version: "v0.2.8"}, []string{"version", "--verbose"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("version --verbose exit code = %d, stderr = %q", code, stderr.String())
	}
	const want = "sanho v0.2.8 (git_sha unknown)\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version --verbose output = %q, want %q", got, want)
	}
}

func TestVersionVerboseAliasesThroughRun(t *testing.T) {
	const (
		version     = "v0.2.8-dev.0123456789ab"
		gitSHA      = "0123456789abcdef0123456789abcdef01234567"
		verboseText = "sanho " + version + " (git_sha " + gitSHA + ")\n"
		plainText   = "sanho " + version + "\n"
	)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "global long before version", args: []string{"--verbose", "version"}, want: verboseText},
		{name: "global short before version", args: []string{"-v", "version"}, want: verboseText},
		{name: "local long after version", args: []string{"version", "--verbose"}, want: verboseText},
		{name: "inherited short after version", args: []string{"version", "-v"}, want: verboseText},
		{name: "subsequent nonverbose run resets", args: []string{"version"}, want: plainText},
	}

	for _, testCase := range cases {
		var stdout, stderr bytes.Buffer
		code := Run(BuildInfo{Version: version, GitSHA: gitSHA}, testCase.args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%s exit code = %d, stderr = %q", testCase.name, code, stderr.String())
		}
		if got := stdout.String(); got != testCase.want {
			t.Fatalf("%s output = %q, want %q", testCase.name, got, testCase.want)
		}
		if stderr.Len() != 0 {
			t.Fatalf("%s stderr = %q, want empty", testCase.name, stderr.String())
		}
	}
}
