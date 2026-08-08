package appgit

import (
	"reflect"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// parseCommitTrailers reads `git log --format=%H%n%B%x00`. Its inputs
// are commit messages, which users write, so the cases below are the
// ones a message can produce rather than the ones a formatter would.

const (
	commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseX   = "1111111111111111111111111111111111111111"
	treeX   = "2222222222222222222222222222222222222222"
	legacyY = "3333333333333333333333333333333333333333"
)

// record renders one commit the way the log format does.
func record(commit, message string) string {
	return commit + "\n" + message + commitRecordSeparator
}

func TestParseCommitTrailersReadsBothKeys(t *testing.T) {
	out := record(commitA, "docs: edit\n\ndocs-base: "+baseX+"\ndocs-base-tree: "+treeX+"\n") +
		record(commitB, "docs: older\n\ndocs-version: "+legacyY+"\n")

	commits := parseCommitTrailers(out)
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if commits[0].Commit != commitA || commits[1].Commit != commitB {
		t.Fatalf("commit OIDs = %q, %q; want %q, %q",
			commits[0].Commit, commits[1].Commit, commitA, commitB)
	}

	wantFirst := map[string][]string{
		provenance.TrailerBase:     {baseX},
		provenance.TrailerBaseTree: {treeX},
	}
	if !reflect.DeepEqual(commits[0].Values, wantFirst) {
		t.Fatalf("first commit trailers = %v, want %v", commits[0].Values, wantFirst)
	}

	wantSecond := map[string][]string{provenance.LegacyTrailerVersion: {legacyY}}
	if !reflect.DeepEqual(commits[1].Values, wantSecond) {
		t.Fatalf("second commit trailers = %v, want %v", commits[1].Values, wantSecond)
	}
}

// The newest stamped commit wins, and a docs-free commit in between does
// not hide the one behind it (the hook contract).
func TestSelectBasePicksTheNewestStampedCommit(t *testing.T) {
	out := record(commitA, "feat: unrelated code\n") +
		record(commitB, "docs: edit\n\ndocs-base: "+baseX+"\ndocs-base-tree: "+treeX+"\n")

	base, ok := provenance.SelectBase(parseCommitTrailers(out))
	if !ok {
		t.Fatal("SelectBase found nothing, want the stamped commit")
	}
	if base.Commit != baseX || base.Tree != treeX {
		t.Fatalf("base = %+v, want {%s %s}", base, baseX, treeX)
	}
}

// A legacy docs-version commit is adopted as a base with no tree: the
// v0.1 trailer never carried one (the provenance contract legacy coexistence).
func TestSelectBaseAdoptsALegacyTrailer(t *testing.T) {
	out := record(commitA, "docs: v0.1 era\n\ndocs-version: "+legacyY+"\n")

	base, ok := provenance.SelectBase(parseCommitTrailers(out))
	if !ok {
		t.Fatal("SelectBase found nothing, want the legacy commit")
	}
	if base.Commit != legacyY || base.Tree != "" {
		t.Fatalf("base = %+v, want {%s <empty tree>}", base, legacyY)
	}
}

// A trailer-looking line that is not at the start of a line is not a
// trailer, so quoting one inside a message body cannot forge a base.
func TestParseCommitTrailersIgnoresIndentedAndQuotedLines(t *testing.T) {
	out := record(commitA, "docs: talk about trailers\n\n"+
		"We write \"docs-base: "+baseX+"\" in commit messages.\n"+
		"  docs-base: "+baseX+"\n")

	commits := parseCommitTrailers(out)
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	if values := commits[0].Values[provenance.TrailerBase]; len(values) != 0 {
		t.Fatalf("trailers = %v, want none for a quoted or indented mention", values)
	}
}

func TestParseCommitTrailersHandlesEmptyOutput(t *testing.T) {
	if commits := parseCommitTrailers(""); len(commits) != 0 {
		t.Fatalf("got %d commits for empty output, want 0", len(commits))
	}
	if _, ok := provenance.SelectBase(parseCommitTrailers("")); ok {
		t.Fatal("SelectBase found a base in empty output")
	}
}

// A commit message ending without a newline still parses: git emits the
// NUL immediately after the body.
func TestParseCommitTrailersHandlesAMissingTrailingNewline(t *testing.T) {
	out := commitA + "\ndocs: edit\n\ndocs-base: " + baseX + commitRecordSeparator

	base, ok := provenance.SelectBase(parseCommitTrailers(out))
	if !ok || base.Commit != baseX {
		t.Fatalf("SelectBase = (%+v, %v), want the base %s", base, ok, baseX)
	}
}
