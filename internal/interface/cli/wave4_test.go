package cli

// Unit regressions for the fourth review wave's Medium findings that are
// decidable without a repository: the trailer-block append (M5) and the
// message renderings the audit named.

import (
	"strings"
	"testing"
)

// TestAppendTrailersExtendsAnExistingTrailerBlock is M5.
//
// Git reads a trailer block as the LAST paragraph of a commit message.
// Always inserting a blank line before the provenance lines therefore
// demoted an existing block — `Signed-off-by:`, `Co-authored-by:`, a
// squash's collected trailers — into ordinary body text, and
// `git interpret-trailers --parse` reported only sanho's own lines from
// then on. Every commit sanho stamped silently stopped having a
// Signed-off-by trailer.
func TestAppendTrailersExtendsAnExistingTrailerBlock(t *testing.T) {
	trailers := []string{"docs-base: 1111111111111111111111111111111111111111"}

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "message ending in a trailer block is extended, not followed",
			message: "docs: edit\n\nSigned-off-by: A <a@example.test>\n",
			want: "docs: edit\n\n" +
				"Signed-off-by: A <a@example.test>\n" +
				"docs-base: 1111111111111111111111111111111111111111\n",
		},
		{
			name: "several existing trailers all stay in one block",
			message: "docs: edit\n\nbody paragraph\n\n" +
				"Signed-off-by: A <a@example.test>\nCo-authored-by: B <b@example.test>\n",
			want: "docs: edit\n\nbody paragraph\n\n" +
				"Signed-off-by: A <a@example.test>\n" +
				"Co-authored-by: B <b@example.test>\n" +
				"docs-base: 1111111111111111111111111111111111111111\n",
		},
		{
			name:    "a folded continuation line is part of the block",
			message: "docs: edit\n\nSigned-off-by: A\n  continued\n",
			want: "docs: edit\n\nSigned-off-by: A\n  continued\n" +
				"docs-base: 1111111111111111111111111111111111111111\n",
		},
		{
			name:    "a prose last paragraph gets the blank line that makes a block",
			message: "docs: edit\n\nthis explains the change\n",
			want: "docs: edit\n\nthis explains the change\n\n" +
				"docs-base: 1111111111111111111111111111111111111111\n",
		},
		{
			name:    "one non-trailer line makes the whole paragraph body text",
			message: "docs: edit\n\nSigned-off-by: A <a@example.test>\nand a note\n",
			want: "docs: edit\n\nSigned-off-by: A <a@example.test>\nand a note\n\n" +
				"docs-base: 1111111111111111111111111111111111111111\n",
		},
		{
			name:    "a subject-only message is not a trailer block, however it is punctuated",
			message: "docs: edit\n",
			want:    "docs: edit\n\ndocs-base: 1111111111111111111111111111111111111111\n",
		},
		{
			name:    "a subject that looks like a trailer is still a subject",
			message: "fix: something\n",
			want:    "fix: something\n\ndocs-base: 1111111111111111111111111111111111111111\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := string(appendTrailers([]byte(test.message), trailers))
			if got != test.want {
				t.Fatalf("appendTrailers =\n%q\nwant\n%q", got, test.want)
			}
		})
	}
}

// TestPushSyncRequiredMessageNamesNoAbsentBase is the `base (none)`
// render the audit called out: a workspace with no recorded base has
// nothing to put on the left of the arrow, and "base (none) → <head>"
// reads as a transition out of a state rather than as the absence of one.
func TestPushSyncRequiredMessageNamesNoAbsentBase(t *testing.T) {
	withBase := pushSyncRequiredMessage("cas_retry_exhausted", sampleBaseOID, sampleHeadOID)
	if !strings.Contains(withBase, "base 67c4bbfeada3 → 9a41f2c0e1d2") {
		t.Fatalf("a recorded base must still be quoted:\n%s", withBase)
	}

	withoutBase := pushSyncRequiredMessage("no_base", "", sampleHeadOID)
	if strings.Contains(withoutBase, "(none)") {
		t.Fatalf("an absent base was rendered as a state:\n%s", withoutBase)
	}
	if !strings.Contains(withoutBase, "canonical head 9a41f2c0e1d2") {
		t.Fatalf("an absent base must still name the head:\n%s", withoutBase)
	}
}

// TestPushEmptyDocsMessageAdvisesASingleUsePrefix is the
// SANHO_ALLOW_DOCS_DELETION persistence finding: the variable is read
// from the process environment, so `export`ing it disarms the refusal
// for every push in that shell — which is not the "for that one push"
// the message used to promise. The prefix form genuinely is single-use.
func TestPushEmptyDocsMessageAdvisesASingleUsePrefix(t *testing.T) {
	message := pushEmptyDocsMessage("legacy", sampleHeadOID, 3)
	if !strings.Contains(message, "SANHO_ALLOW_DOCS_DELETION=1 git push") {
		t.Fatalf("the escape hatch is not shown as a one-command prefix:\n%s", message)
	}
	if strings.Contains(message, "in the environment for that one push") {
		t.Fatalf("the message still promises single-use for an exported variable:\n%s", message)
	}
}

// TestSyncCompletedMessageReportsMergeDrift is W2's informational line.
func TestSyncCompletedMessageReportsMergeDrift(t *testing.T) {
	if got := syncCompletedMessage(sampleBaseOID, 0); strings.Contains(got, "merge result") {
		t.Fatalf("a completion that matches the merge said something about drift:\n%s", got)
	}
	got := syncCompletedMessage(sampleBaseOID, 2)
	if !strings.Contains(got, "2 files differ from the merge result") {
		t.Fatalf("drift = %q, want the count and the reason", got)
	}
	if !strings.Contains(syncCompletedMessage(sampleBaseOID, 1), "1 file differ") {
		t.Fatal("a single-file drift must not be pluralized as a count")
	}
}
