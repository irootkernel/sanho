package publish_test

import (
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/domain/publish"
)

// Fixed OIDs standing in for canonical commits and docs trees. Only
// their (in)equality matters to Decide.
const (
	oidBase    = "1111111111111111111111111111111111111111"
	oidHead    = "2222222222222222222222222222222222222222"
	treeHead   = "3333333333333333333333333333333333333333"
	treeTip    = "4444444444444444444444444444444444444444"
	oidUnknown = "5555555555555555555555555555555555555555"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name string
		in   publish.Inputs
		want publish.Case
	}{
		{
			name: "tip tree equals head tree is up to date",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidBase},
				TipDocsTree:    treeHead,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      true,
				BaseIsAncestor: true,
			},
			want: publish.CaseUpToDate,
		},
		{
			name: "up to date wins over an unknown base",
			in: publish.Inputs{
				Base:         provenance.Base{Commit: oidUnknown},
				TipDocsTree:  treeHead,
				Head:         oidHead,
				HeadDocsTree: treeHead,
			},
			want: publish.CaseUpToDate,
		},
		{
			name: "up to date wins over a known but non-ancestor base",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidBase},
				TipDocsTree:    treeHead,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      true,
				BaseIsAncestor: false,
			},
			want: publish.CaseUpToDate,
		},
		{
			name: "base at head with a differing tip fast-forwards",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidHead},
				TipDocsTree:    treeTip,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      true,
				BaseIsAncestor: true,
			},
			want: publish.CaseFastForward,
		},
		{
			name: "base a proper ancestor of head auto-merges",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidBase},
				TipDocsTree:    treeTip,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      true,
				BaseIsAncestor: true,
			},
			want: publish.CaseAutoMerge,
		},
		{
			name: "base absent from canonical is an unknown base",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidUnknown},
				TipDocsTree:    treeTip,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      false,
				BaseIsAncestor: false,
			},
			want: publish.CaseUnknownBase,
		},
		{
			name: "base present but off the branch is an unknown base",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidBase},
				TipDocsTree:    treeTip,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      true,
				BaseIsAncestor: false,
			},
			want: publish.CaseUnknownBase,
		},
		{
			name: "base equal to head but unresolvable is an unknown base",
			in: publish.Inputs{
				Base:           provenance.Base{Commit: oidHead},
				TipDocsTree:    treeTip,
				Head:           oidHead,
				HeadDocsTree:   treeHead,
				BaseKnown:      false,
				BaseIsAncestor: true,
			},
			want: publish.CaseUnknownBase,
		},
		{
			name: "empty base with differing trees is an unknown base",
			in: publish.Inputs{
				TipDocsTree:  treeTip,
				Head:         oidHead,
				HeadDocsTree: treeHead,
			},
			want: publish.CaseUnknownBase,
		},
		{
			name: "empty trees on both sides are up to date",
			in:   publish.Inputs{},
			want: publish.CaseUpToDate,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := publish.Decide(test.in); got != test.want {
				t.Fatalf("Decide() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestDecideIsTotal walks the whole boolean/equality input space and
// asserts every combination lands in exactly one of the four known
// cases: Decide is specified as a total function (the publication contract).
func TestDecideIsTotal(t *testing.T) {
	trees := []string{treeHead, treeTip}
	baseCommits := []string{oidHead, oidBase, ""}
	known := []bool{true, false}
	ancestor := []bool{true, false}

	valid := map[publish.Case]bool{
		publish.CaseUpToDate:    true,
		publish.CaseFastForward: true,
		publish.CaseAutoMerge:   true,
		publish.CaseUnknownBase: true,
	}

	for _, tip := range trees {
		for _, baseCommit := range baseCommits {
			for _, isKnown := range known {
				for _, isAncestor := range ancestor {
					in := publish.Inputs{
						Base:           provenance.Base{Commit: baseCommit},
						TipDocsTree:    tip,
						Head:           oidHead,
						HeadDocsTree:   treeHead,
						BaseKnown:      isKnown,
						BaseIsAncestor: isAncestor,
					}
					got := publish.Decide(in)
					if !valid[got] {
						t.Fatalf("Decide(%+v) = %v, which is not a known case", in, got)
					}
					if got == publish.CaseUpToDate && tip != treeHead {
						t.Fatalf("Decide(%+v) = up to date for a differing tip tree", in)
					}
					if got == publish.CaseFastForward && baseCommit != oidHead {
						t.Fatalf("Decide(%+v) = fast forward for a base that is not head", in)
					}
				}
			}
		}
	}
}

func TestCaseString(t *testing.T) {
	tests := []struct {
		in   publish.Case
		want string
	}{
		{publish.CaseUpToDate, "up_to_date"},
		{publish.CaseFastForward, "fast_forward"},
		{publish.CaseAutoMerge, "auto_merge"},
		{publish.CaseUnknownBase, "unknown_base"},
		{publish.Case(42), "case(42)"},
	}
	for _, test := range tests {
		if got := test.in.String(); got != test.want {
			t.Errorf("Case(%d).String() = %q, want %q", int(test.in), got, test.want)
		}
	}
}

func TestCommitMetaSubject(t *testing.T) {
	tests := []struct {
		name string
		meta publish.CommitMeta
		want string
	}{
		{
			name: "two commits",
			meta: publish.CommitMeta{RepoName: "sanho", Branch: "main", Subjects: []string{"a", "b"}},
			want: "[SANHO] Publish docs from sanho/main (2 app commits)",
		},
		{
			name: "one commit keeps the fixed plural wording",
			meta: publish.CommitMeta{RepoName: "sanho", Branch: "main", Subjects: []string{"a"}},
			want: "[SANHO] Publish docs from sanho/main (1 app commits)",
		},
		{
			name: "no commits",
			meta: publish.CommitMeta{RepoName: "sanho", Branch: "feature/x"},
			want: "[SANHO] Publish docs from sanho/feature/x (0 app commits)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.meta.Subject(); got != test.want {
				t.Fatalf("Subject() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCommitMetaMessage(t *testing.T) {
	meta := publish.CommitMeta{
		RepoName:    "sanho",
		Branch:      "main",
		WorkspaceID: "sanho:/home/u/sanho",
		TipOID:      oidHead,
		Subjects:    []string{"docs: add api guide", "docs: fix typo"},
	}

	want := "[SANHO] Publish docs from sanho/main (2 app commits)\n" +
		"\n" +
		"source: sanho:/home/u/sanho @ " + oidHead + "\n" +
		"commits:\n" +
		"  - docs: add api guide\n" +
		"  - docs: fix typo\n"

	if got := meta.Message(); got != want {
		t.Fatalf("Message() =\n%q\nwant\n%q", got, want)
	}
}

func TestCommitMetaMessageOmitsEmptyCommitsSection(t *testing.T) {
	meta := publish.CommitMeta{
		RepoName:    "sanho",
		Branch:      "main",
		WorkspaceID: "ws",
		TipOID:      oidHead,
	}

	got := meta.Message()
	if strings.Contains(got, "commits:") {
		t.Fatalf("Message() rendered a dangling commits header:\n%s", got)
	}
	want := "[SANHO] Publish docs from sanho/main (0 app commits)\n\nsource: ws @ " + oidHead + "\n"
	if got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
}

func TestCommitMetaMessageStartsWithSubjectAndBlankLine(t *testing.T) {
	meta := publish.CommitMeta{RepoName: "r", Branch: "b", WorkspaceID: "w", TipOID: oidHead}
	lines := strings.Split(meta.Message(), "\n")
	if len(lines) < 3 {
		t.Fatalf("Message() has too few lines: %q", meta.Message())
	}
	if lines[0] != meta.Subject() {
		t.Errorf("first line = %q, want the subject %q", lines[0], meta.Subject())
	}
	if lines[1] != "" {
		t.Errorf("second line = %q, want a blank separator line", lines[1])
	}
}
