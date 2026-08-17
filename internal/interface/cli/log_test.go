package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/infra/canonical"
)

func TestLogOptionsRejectUnusableArguments(t *testing.T) {
	tests := map[string]logOptions{
		"zero count":       {maxCount: 0},
		"negative count":   {maxCount: -1},
		"absolute path":    {maxCount: 1, docsPath: "/etc/passwd"},
		"escaping path":    {maxCount: 1, docsPath: "../secrets.md"},
		"docs root itself": {maxCount: 1, docsPath: "."},
		"bare separator":   {maxCount: 1, docsPath: "/"},
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := opts.normalize()
			if err == nil {
				t.Fatalf("normalize(%+v) succeeded, want an error", opts)
			}
			// Every refusal here is the caller's flag combination, so the
			// --json envelope must carry invalid_arguments rather than
			// reporting a sanho defect.
			if !errors.Is(err, errInvalidArguments) {
				t.Errorf("normalize(%+v) error = %v, want errInvalidArguments", opts, err)
			}
		})
	}
}

func TestLogOptionsNormalizeAcceptedPaths(t *testing.T) {
	tests := map[string]struct {
		in   string
		want string
	}{
		"no filter":         {in: "", want: ""},
		"plain document":    {in: "api.md", want: "api.md"},
		"nested document":   {in: "guides/api.md", want: "guides/api.md"},
		"trailing slash":    {in: "guides/", want: "guides"},
		"redundant segment": {in: "guides/./api.md", want: "guides/api.md"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			query, _, err := logOptions{maxCount: 1, docsPath: test.in}.normalize()
			if err != nil {
				t.Fatalf("normalize(%q) error = %v", test.in, err)
			}
			if query.Path != test.want {
				t.Errorf("normalize(%q) = %q, want %q", test.in, query.Path, test.want)
			}
		})
	}
}

// TestLogOptionsBuildTheMessageQuery pins the two halves a source filter
// has: the literals git narrows on, and the fields the decode confirms
// against. They must describe the same request.
func TestLogOptionsBuildTheMessageQuery(t *testing.T) {
	query, filter, err := logOptions{
		maxCount:   5,
		repository: "app",
		workspace:  "product:/home/u/app",
	}.normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	want := []string{
		"[SANHO] Publish docs from app/",
		"source: product:/home/u/app @ ",
	}
	if !reflect.DeepEqual(query.Message, want) {
		t.Errorf("query.Message = %q, want %q", query.Message, want)
	}
	if filter.repository != "app" || filter.workspace != "product:/home/u/app" {
		t.Errorf("filter = %+v, want both fields carried through", filter)
	}

	// No filter, no grep: an unnarrowed listing must not pay for one.
	plain, plainFilter, err := logOptions{maxCount: 5}.normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(plain.Message) != 0 {
		t.Errorf("query.Message = %q, want none", plain.Message)
	}
	if plainFilter.active() {
		t.Errorf("filter = %+v, want it inactive", plainFilter)
	}
}

// TestBuildLogJSONConfirmsWhatGitNarrowed is the reason the filter is
// applied twice. git greps message TEXT, so a commit can carry the
// literal for a reason of its own; membership is decided on the decoded
// fields, and an external commit — which has no source at all — never
// belongs to a source filter.
func TestBuildLogJSONConfirmsWhatGitNarrowed(t *testing.T) {
	committedAt := time.Date(2026, 8, 14, 9, 14, 3, 0, time.UTC)
	publication := func(commit, repo, workspace string) canonical.LogEntry {
		return canonical.LogEntry{
			Commit:      commit,
			Tree:        "1111111111111111111111111111111111111111",
			CommittedAt: committedAt,
			Message: "[SANHO] Publish docs from " + repo + "/main (1 app commits)\n\n" +
				"source: " + workspace + " @ 67c4bbfeada37f5dda8fb79aa43216ef062cd8df\n" +
				"commits:\n  - docs: update api\n",
		}
	}
	entries := []canonical.LogEntry{
		publication("1000000000000000000000000000000000000000", "app", "product:/home/u/app"),
		publication("2000000000000000000000000000000000000000", "site", "product:/home/u/site"),
		{
			// A hand-written canonical commit that happens to quote the
			// subject line git greps for. It decodes to nothing, so it
			// belongs to no source.
			Commit:      "3000000000000000000000000000000000000000",
			Tree:        "1111111111111111111111111111111111111111",
			CommittedAt: committedAt,
			Message:     "docs: mention [SANHO] Publish docs from app/main in the handbook\n",
		},
	}

	only := buildLogJSON("main", 0, true, entries, logFilter{repository: "app"})
	if len(only.Entries) != 1 {
		t.Fatalf("repository filter kept %d entries, want 1: %+v", len(only.Entries), only.Entries)
	}
	if only.Entries[0].Commit != "1000000000000000000000000000000000000000" {
		t.Errorf("kept %q, want the app publication", only.Entries[0].Commit)
	}

	// Two filters are AND, not OR.
	crossed := buildLogJSON("main", 0, true, entries,
		logFilter{repository: "app", workspace: "product:/home/u/site"})
	if len(crossed.Entries) != 0 {
		t.Errorf("mismatched repository and workspace kept %+v, want nothing", crossed.Entries)
	}

	// Unfiltered, every commit is still listed, external included.
	all := buildLogJSON("main", 0, true, entries, logFilter{})
	if len(all.Entries) != 3 {
		t.Fatalf("unfiltered listing kept %d entries, want 3", len(all.Entries))
	}
	if all.Entries[2].Kind != logKindExternal {
		t.Errorf("hand-written commit kind = %q, want %q", all.Entries[2].Kind, logKindExternal)
	}
}

func TestLogNarrowingNamesTheActiveFilters(t *testing.T) {
	tests := map[string]struct {
		opts logOptions
		want string
	}{
		"none":      {opts: logOptions{}, want: ""},
		"path":      {opts: logOptions{docsPath: "api.md"}, want: "path api.md"},
		"source":    {opts: logOptions{repository: "app"}, want: "repository app"},
		"workspace": {opts: logOptions{workspace: "p:/w"}, want: "workspace p:/w"},
		"all three": {
			opts: logOptions{docsPath: "api.md", repository: "app", workspace: "p:/w"},
			want: "path api.md and repository app and workspace p:/w",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := test.opts.narrowing(); got != test.want {
				t.Errorf("narrowing() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestBuildLogJSONSeparatesPublicationsFromForeignCommits is the reason
// the entry carries a kind at all: canonical history holds commits made
// directly in the docs repository, and reporting a source for one would
// assert provenance nothing recorded.
func TestBuildLogJSONSeparatesPublicationsFromForeignCommits(t *testing.T) {
	committedAt := time.Date(2026, 8, 14, 9, 14, 3, 0, time.UTC)
	entries := []canonical.LogEntry{
		{
			Commit:      "9a41f2c0e1d2c3b4a5968778695a4b3c2d1e0f9a",
			Tree:        "1111111111111111111111111111111111111111",
			CommittedAt: committedAt,
			Message: "[SANHO] Publish docs from app/main (2 app commits)\n\n" +
				"source: product:/home/u/app @ 67c4bbfeada37f5dda8fb79aa43216ef062cd8df\n" +
				"commits:\n  - docs: update api\n  - docs: fix typo\n",
		},
		{
			Commit:      "2222222222222222222222222222222222222222",
			Tree:        "3333333333333333333333333333333333333333",
			CommittedAt: committedAt,
			Message:     "docs: hand-edited in the canonical repository\n\nwith a body\n",
		},
	}

	document := buildLogJSON("main", 12*time.Second, true, entries, logFilter{})
	if document.Branch != "main" || !document.FetchedEver || document.DataAgeSeconds != 12 {
		t.Errorf("canonical facts = %+v, want branch main, fetched, 12s old", document)
	}
	if len(document.Entries) != 2 {
		t.Fatalf("buildLogJSON returned %d entries, want 2", len(document.Entries))
	}

	published := document.Entries[0]
	if published.Kind != logKindPublication {
		t.Errorf("kind = %q, want %q", published.Kind, logKindPublication)
	}
	if published.Source == nil {
		t.Fatal("a publication reported no source")
	}
	if published.Source.Repository != "app" || published.Source.Branch != "main" {
		t.Errorf("source repository/branch = %q/%q, want app/main",
			published.Source.Repository, published.Source.Branch)
	}
	if published.Source.WorkspaceID != "product:/home/u/app" {
		t.Errorf("source workspace = %q, want product:/home/u/app", published.Source.WorkspaceID)
	}
	if published.Source.ApplicationCommit != "67c4bbfeada37f5dda8fb79aa43216ef062cd8df" {
		t.Errorf("application commit = %q, want the full OID", published.Source.ApplicationCommit)
	}
	if len(published.ApplicationSubjects) != 2 {
		t.Errorf("application subjects = %q, want two", published.ApplicationSubjects)
	}
	if published.CommittedAt != "2026-08-14T09:14:03Z" {
		t.Errorf("committed_at = %q, want RFC3339", published.CommittedAt)
	}
	if strings.Contains(published.Subject, "\n") {
		t.Errorf("subject = %q, want only the first line", published.Subject)
	}

	external := document.Entries[1]
	if external.Kind != logKindExternal {
		t.Errorf("kind = %q, want %q", external.Kind, logKindExternal)
	}
	if external.Source != nil {
		t.Errorf("source = %+v, want null for a foreign commit", external.Source)
	}
	// The JSON contract: an empty array, never null.
	if external.ApplicationSubjects == nil {
		t.Error("application_subjects is nil, want an empty array")
	}
	if external.Subject != "docs: hand-edited in the canonical repository" {
		t.Errorf("subject = %q, want the first line only", external.Subject)
	}
}
