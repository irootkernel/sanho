package cli

// The JSON contract's envelope rule at the argument boundary, and the
// unknown-command refusal cobra applies only to the root.
//
// Every case here fails before RunE, so none of them touch the
// filesystem, the registry, or git. The one exception is `check --json`,
// which reaches RunE and refuses on its own policy combination before
// resolving a workspace — it is here as the guard that the boundary does
// not write a second envelope over the one the command already wrote.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// runCLI drives one invocation the way Execute does, capturing both
// channels.
func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(BuildInfo{Version: "v0.0.0-test"}, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// requireEnvelope parses stdout as exactly one error envelope and
// returns its code.
func requireEnvelope(t *testing.T, label, stdout string) string {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(stdout))
	var envelope errorJSON
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("%s: parse envelope: %v\nstdout: %q", label, err, stdout)
	}
	// The JSON contract is one document per invocation. A second one
	// here would mean two writers rendered the same failure.
	if decoder.More() {
		t.Fatalf("%s: stdout carries more than one document\nstdout: %q", label, stdout)
	}
	if envelope.Error.Message == "" {
		t.Errorf("%s: envelope has no message\nstdout: %q", label, stdout)
	}
	return envelope.Error.Code
}

// TestArgumentFailuresWriteTheJSONEnvelope covers the defect the JSON
// contract names: a --json command that fails writes an envelope, and
// that has to hold for the failures cobra detects before the command
// runs, not only for the ones the command reports itself.
func TestArgumentFailuresWriteTheJSONEnvelope(t *testing.T) {
	// Every machine-readable command of docs/cli-json.md, each carrying
	// whatever flags or arguments it needs to get past its own
	// validation, so the argument failure is the only thing left to
	// report. The list is not counted in prose: it went stale twice by
	// being, and what it has to be is complete.
	//
	// `show` is the one entry that supplies a positional rather than a
	// flag, and it is the reason classifyArgumentErrors keys on "declares
	// an Args rule" rather than on cobra.NoArgs: its rule is
	// cobra.ExactArgs(1), and the envelope is owed all the same.
	commands := [][]string{
		{"status"},
		{"state"},
		{"log"},
		{"show", "deadbeef"},
		{"preview"},
		{"check", "--require-clean"},
		{"sync"},
		{"pull"},
		{"doctor"},
		{"version"},
	}
	// The three shapes cobra rejects: an argument no command accepts, a
	// flag no command declares, and a value a declared flag cannot parse.
	failures := []struct {
		name string
		args []string
	}{
		{"unexpected argument", []string{"extra"}},
		{"unknown flag", []string{"--no-such-flag"}},
	}

	for _, command := range commands {
		for _, failure := range failures {
			args := append(append([]string{}, command...), "--json")
			args = append(args, failure.args...)
			label := "sanho " + strings.Join(args, " ")

			code, stdout, stderr := runCLI(args...)

			if code != exitUser {
				t.Errorf("%s: exit = %d, want %d", label, code, exitUser)
			}
			if got := requireEnvelope(t, label, stdout); got != codeInvalidArguments {
				t.Errorf("%s: error.code = %q, want %q", label, got, codeInvalidArguments)
			}
			if !strings.HasPrefix(stderr, errorPrefix) {
				t.Errorf("%s: stderr = %q, want the human line too", label, stderr)
			}
		}
	}
}

// TestUnparseableFlagValueWritesTheJSONEnvelope is the third failure
// shape, which needs a flag that takes a value.
func TestUnparseableFlagValueWritesTheJSONEnvelope(t *testing.T) {
	code, stdout, stderr := runCLI("log", "--json", "-n", "not-a-number")

	if code != exitUser {
		t.Errorf("exit = %d, want %d", code, exitUser)
	}
	if got := requireEnvelope(t, "log --json -n not-a-number", stdout); got != codeInvalidArguments {
		t.Errorf("error.code = %q, want %q", got, codeInvalidArguments)
	}
	if !strings.Contains(stderr, "max-count") {
		t.Errorf("stderr = %q, want the rejected flag named", stderr)
	}
}

// TestJSONEnvelopeSurvivesAFlagErrorBeforeTheJSONFlag pins the reason
// jsonRequested reads raw argv at all: pflag stops at the first token it
// cannot handle, so --json after the mistake is never applied, and the
// contract would otherwise hold only for one argument order.
func TestJSONEnvelopeSurvivesAFlagErrorBeforeTheJSONFlag(t *testing.T) {
	code, stdout, _ := runCLI("status", "--no-such-flag", "--json")

	if code != exitUser {
		t.Errorf("exit = %d, want %d", code, exitUser)
	}
	if got := requireEnvelope(t, "status --no-such-flag --json", stdout); got != codeInvalidArguments {
		t.Errorf("error.code = %q, want %q", got, codeInvalidArguments)
	}
}

// TestUnparseableJSONFlagValueWritesTheJSONEnvelope is the case that
// distinguishes "asked for prose" from "asked for nothing parseable".
// `--json=false` is a choice and gets prose; `--json=maybe` is the
// failure being reported, and reporting it on an empty stdout would
// break the contract for the flag the invocation was engaging.
func TestUnparseableJSONFlagValueWritesTheJSONEnvelope(t *testing.T) {
	code, stdout, stderr := runCLI("status", "--json=maybe")

	if code != exitUser {
		t.Errorf("exit = %d, want %d", code, exitUser)
	}
	if got := requireEnvelope(t, "status --json=maybe", stdout); got != codeInvalidArguments {
		t.Errorf("error.code = %q, want %q", got, codeInvalidArguments)
	}
	if !strings.Contains(stderr, "maybe") {
		t.Errorf("stderr = %q, want the rejected value named", stderr)
	}
}

// TestArgumentFailuresStayProseWithoutJSON keeps the envelope off stdout
// where no machine asked for one. An envelope in a human's terminal, or
// on the stdout of a command with no JSON document at all, is noise.
func TestArgumentFailuresStayProseWithoutJSON(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no --json at all", []string{"status", "extra"}},
		{"--json explicitly disabled", []string{"status", "--json=false", "extra"}},
		{"command with no JSON document", []string{"project", "add", "--json", "example"}},
		{"unknown command at the root", []string{"no-such-command"}},
		{"unknown subcommand in a group", []string{"project", "no-such-subcommand"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(testCase.args...)

			if code != exitUser {
				t.Errorf("exit = %d, want %d", code, exitUser)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if !strings.HasPrefix(stderr, errorPrefix) {
				t.Errorf("stderr = %q, want the human line", stderr)
			}
		})
	}
}

// TestPolicyArgumentFailureWritesOneEnvelope is the double-write guard.
// `check --json` with no policy selected already renders its own
// envelope from inside RunE; the argument boundary must not render a
// second one over it.
func TestPolicyArgumentFailureWritesOneEnvelope(t *testing.T) {
	code, stdout, stderr := runCLI("check", "--json")

	if code != exitUser {
		t.Errorf("exit = %d, want %d", code, exitUser)
	}
	if got := requireEnvelope(t, "check --json", stdout); got != codeInvalidArguments {
		t.Errorf("error.code = %q, want %q", got, codeInvalidArguments)
	}
	if !strings.Contains(stderr, "--require-clean") {
		t.Errorf("stderr = %q, want the policy guidance", stderr)
	}
}

// TestGroupsRefuseAnUnknownSubcommand covers the second defect: cobra
// reaches its unknown-command check through legacyArgs, which is guarded
// by !HasParent(), so a group resolved the typo, found itself
// unrunnable, printed help and exited 0.
func TestGroupsRefuseAnUnknownSubcommand(t *testing.T) {
	for _, group := range []string{"project", "workspace", "hook"} {
		code, stdout, stderr := runCLI(group, "no-such-subcommand")

		if code != exitUser {
			t.Errorf("sanho %s no-such-subcommand: exit = %d, want %d", group, code, exitUser)
		}
		if stdout != "" {
			t.Errorf("sanho %s no-such-subcommand: stdout = %q, want nothing", group, stdout)
		}
		want := "unknown command \"no-such-subcommand\" for \"sanho " + group + "\""
		if !strings.Contains(stderr, want) {
			t.Errorf("sanho %s no-such-subcommand: stderr = %q, want %q", group, stderr, want)
		}
	}
}

// TestGroupSuggestsANearSubcommand is why the refusal is cobra's own
// wording rather than a bare rejection: the root answers a typo with the
// command it meant, and a group now answers the same way.
func TestGroupSuggestsANearSubcommand(t *testing.T) {
	_, _, stderr := runCLI("project", "ad")

	for _, want := range []string{"Did you mean this?", "add"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("sanho project ad: stderr = %q, want %q in it", stderr, want)
		}
	}
}

// TestGroupsStillListTheirSubcommands keeps the other half of the group
// contract: naming a group alone is a request for its help, not a
// mistake.
func TestGroupsStillListTheirSubcommands(t *testing.T) {
	for _, group := range []string{"project", "workspace", "hook"} {
		code, stdout, stderr := runCLI(group)

		if code != exitSuccess {
			t.Errorf("sanho %s: exit = %d, want %d\nstderr: %s", group, code, exitSuccess, stderr)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Errorf("sanho %s: stdout = %q, want the group's help", group, stdout)
		}
	}
}

// TestRootKeepsItsOwnUnknownCommandReport pins the reason
// classifyArgumentErrors skips a command whose Args is nil: cobra's Find
// only consults legacyArgs while Args is nil, and legacyArgs is what
// makes the root refuse an unknown command instead of printing help.
func TestRootKeepsItsOwnUnknownCommandReport(t *testing.T) {
	code, stdout, stderr := runCLI("statu")

	if code != exitUser {
		t.Errorf("exit = %d, want %d", code, exitUser)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	for _, want := range []string{"unknown command \"statu\" for \"sanho\"", "Did you mean this?", "status"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q in it", stderr, want)
		}
	}
}

func TestArgsRequestJSON(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"absent", []string{"status", "--refresh"}, false},
		{"bare", []string{"status", "--json"}, true},
		{"explicit true", []string{"status", "--json=true"}, true},
		{"explicit false", []string{"status", "--json=false"}, false},
		// A value pflag cannot parse is the failure being reported, so
		// it is owed an envelope; only a parseable false opts out.
		{"unparseable value", []string{"status", "--json=maybe"}, true},
		{"after the flag terminator", []string{"status", "--", "--json"}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := argsRequestJSON(testCase.args); got != testCase.want {
				t.Errorf("argsRequestJSON(%q) = %v, want %v", testCase.args, got, testCase.want)
			}
		})
	}
}
