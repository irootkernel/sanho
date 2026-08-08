package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/usecase/publish"
)

// The §5.9 templates are normative: the design document fixes their
// wording, and agents and humans both read them. These tests pin the
// three of them character for character, so a well-meaning rewording
// fails here rather than silently changing a contract.

func TestCommitWarningMatchesTemplate1(t *testing.T) {
	got := commitBehindClean(2)
	want := "sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly"
	if got != want {
		t.Fatalf("commitBehindClean(2) =\n%q\nwant\n%q", got, want)
	}
}

func TestCommitWarningConflictVariantSaysWhatHappensIfIgnored(t *testing.T) {
	got := commitBehindConflicts(3, []string{"docs/api.md", "docs/schema.md"})
	want := "sanho: docs base is 3 commits behind — 'sanho sync' will report conflicts in " +
		"docs/api.md, docs/schema.md; syncing sooner keeps them small"
	if got != want {
		t.Fatalf("commitBehindConflicts =\n%q\nwant\n%q", got, want)
	}
}

// Template 2 gains one line, and the deviation is deliberate: §5.5's
// D3 ("no new vocabulary — resolve, add, commit") could not be kept
// without keeping the inference that made a stash indistinguishable from
// a resolution. `sanho sync --continue` is the new step, it is named
// where the conflict is created, and docs/architecture.md records the
// deviation. The rest of the template is still character for character.
func TestSyncConflictMatchesTemplate2(t *testing.T) {
	got := syncConflictMessage("docs", []string{"docs/api.md", "docs/schema.md"})
	want := strings.Join([]string{
		"sanho: merged docs with upstream — 2 files have conflicts:",
		"  docs/api.md",
		"  docs/schema.md",
		"Resolve the markers, then:  git add docs/ && git commit",
		"Then complete the sync:     sanho sync --continue",
		"To undo this sync:          sanho sync --abort",
	}, "\n")
	if got != want {
		t.Fatalf("syncConflictMessage =\n%s\n\nwant\n%s", got, want)
	}
}

// The docs directory is the workspace's configured one; the template's
// `docs/` is the default, not a hardcoded path.
func TestSyncConflictUsesTheConfiguredDocsDirectory(t *testing.T) {
	got := syncConflictMessage("documentation", []string{"documentation/api.md"})
	if !strings.Contains(got, "git add documentation/ && git commit") {
		t.Fatalf("syncConflictMessage =\n%s\nwant it to name the configured docs directory", got)
	}
}

func TestPushRejectionMatchesTemplate3(t *testing.T) {
	base := "67c4bbfeada37f5dda8fb79aa43216ef062cd8df"
	head := "9a41f2cbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	got := pushConflictMessage(base, head, []string{"docs/api.md", "docs/schema.md"})
	want := strings.Join([]string{
		// §5.9's hygiene rule fixes the OID width at 12; the template's
		// 7-character example is illustrative.
		"sanho: your docs changes conflict with upstream (base 67c4bbfeada3 → 9a41f2cbbbbb)",
		// The conflicted files, indented as in templates 2 and the
		// marker listing. The rejection already knew them; leaving them
		// out sent the user to run `sanho sync` just to find out which.
		"  docs/api.md",
		"  docs/schema.md",
		"Run 'sanho sync', resolve, commit, then push again.",
		"error: push rejected — no remote ref was changed",
	}, "\n")
	if got != want {
		t.Fatalf("pushConflictMessage =\n%s\n\nwant\n%s", got, want)
	}
}

// A rejection with no file-level detail — an exhausted CAS budget
// reaches the same renderer — prints §5.9 template 3 unchanged.
func TestPushRejectionWithoutFileDetailIsTheBareTemplate(t *testing.T) {
	got := pushConflictMessage("a", "b", nil)
	want := strings.Join([]string{
		"sanho: your docs changes conflict with upstream (base a → b)",
		"Run 'sanho sync', resolve, commit, then push again.",
		"error: push rejected — no remote ref was changed",
	}, "\n")
	if got != want {
		t.Fatalf("pushConflictMessage =\n%s\n\nwant\n%s", got, want)
	}
}

// The X1 state has to say plainly what it is, because it is the one
// refusal whose docs look finished — and it must name every way out,
// including the one that says "my version is the resolution".
func TestSyncNotCommittedNamesEveryWayOut(t *testing.T) {
	got := syncNotCommittedMessage(sampleBaseOID, sampleHeadOID)
	for _, want := range []string{
		"no commit has changed the files it conflicted on",
		"sanho sync --abort",
		"sanho sync --continue",
		"stays in your stash",
		"67c4bbfeada3",
		"9a41f2c0e1d2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("syncNotCommittedMessage =\n%s\nwant it to contain %q", got, want)
		}
	}
	// Template 2's advice is exactly what does NOT work here: there are
	// no markers left to resolve and nothing staged to commit.
	if strings.Contains(got, "git add") {
		t.Errorf("syncNotCommittedMessage =\n%s\nnames a commit that has nothing to commit", got)
	}
}

// The two unfinished-but-fine states share a renderer, so the wording
// that separates them is worth pinning: one asserts a commit exists, the
// other asserts none does, and neither may claim the other.
func TestSyncNeedsContinueDistinguishesTheTwoStates(t *testing.T) {
	committed := syncNeedsContinueMessage(sampleBaseOID, sampleHeadOID, true)
	uncommitted := syncNeedsContinueMessage(sampleBaseOID, sampleHeadOID, false)

	for _, message := range []string{committed, uncommitted} {
		for _, want := range []string{"is not completed — ", "sanho sync --continue", "sanho sync --abort"} {
			if !strings.Contains(message, want) {
				t.Errorf("message =\n%s\nwant it to contain %q", message, want)
			}
		}
	}
	if !strings.Contains(committed, "the resolution is committed") {
		t.Errorf("committed rendering =\n%s\nwant it to say the resolution is committed", committed)
	}
	if !strings.Contains(uncommitted, "Commit your resolution") {
		t.Errorf("uncommitted rendering =\n%s\nwant it to name the commit that is missing", uncommitted)
	}
	// The abort throws away an uncommitted resolution, and only the
	// rendering printed while one might exist has to say so.
	if !strings.Contains(uncommitted, "discards anything you have not committed") {
		t.Errorf("uncommitted rendering =\n%s\nwant it to say what the abort discards", uncommitted)
	}
	// Neither may borrow the "put aside" explanation, which is a claim
	// about commits that this renderer never checks.
	for _, message := range []string{committed, uncommitted} {
		if strings.Contains(message, "no commit has changed the files it conflicted on") {
			t.Errorf("message =\n%s\nstates a reason it does not know", message)
		}
	}
}

// §5.9 template 2 names the resolution sequence in order, and the order
// is the message: commit, then complete, or undo.
func TestConflictTemplateNamesTheWholeSequence(t *testing.T) {
	got := syncConflictMessage("docs", []string{"docs/api.md"})
	commit := strings.Index(got, "git add docs/ && git commit")
	proceed := strings.Index(got, "sanho sync --continue")
	abort := strings.Index(got, "sanho sync --abort")

	switch {
	case commit < 0 || proceed < 0 || abort < 0:
		t.Fatalf("syncConflictMessage =\n%s\nwant it to name the commit, the completion and the undo", got)
	case commit > proceed:
		t.Errorf("syncConflictMessage =\n%s\nnames --continue before the commit it comes after", got)
	case proceed > abort:
		t.Errorf("syncConflictMessage =\n%s\nwant the undo listed last, as the alternative", got)
	}
	// The blocked-commit rendering of the same template says the same
	// three things, so a user who meets either one reads one procedure.
	blocked := unresolvedSyncMessage("docs", []string{"docs/api.md"})
	for _, want := range []string{"git add docs/ && git commit", "sanho sync --continue", "sanho sync --abort"} {
		if !strings.Contains(blocked, want) {
			t.Errorf("unresolvedSyncMessage =\n%s\nwant it to contain %q", blocked, want)
		}
	}
}

// §5.3 case ④: an anchor that exists is named so the advised command
// runs as printed; when none exists, no command is advised at all.
func TestRewrittenGuidanceOnlyNamesARunnableCommand(t *testing.T) {
	base := "67c4bbfeada37f5dda8fb79aa43216ef062cd8df"
	anchor := "1111111111111111111111111111111111111111"

	withAnchor := pushRewrittenMessage(base, anchor, "/repo/.git/sanho/canonical", "main")
	if !strings.Contains(withAnchor, "sanho sync --rebase-onto "+anchor) {
		t.Fatalf("message with an anchor =\n%s\nwant it to name the anchor commit", withAnchor)
	}
	if strings.Contains(withAnchor, "manual intervention required") {
		t.Fatalf("message with an anchor =\n%s\nwant no manual-intervention text", withAnchor)
	}

	withoutAnchor := pushRewrittenMessage(base, "", "/repo/.git/sanho/canonical", "main")
	if !strings.Contains(withoutAnchor, "manual intervention required") {
		t.Fatalf("message without an anchor =\n%s\nwant it to say manual intervention is required", withoutAnchor)
	}
	// It must still tell the user how to find a target.
	if !strings.Contains(withoutAnchor, "git -C /repo/.git/sanho/canonical log") {
		t.Fatalf("message without an anchor =\n%s\nwant it to show how to list candidates", withoutAnchor)
	}

	// And the ref it names must be one the private clone actually has.
	// The clone is `git init --bare` plus a fetch (§5.2), so it carries
	// refs/remotes/origin/<branch> and no origin/HEAD: naming HEAD made
	// the advised command exit 128 in the state that printed it.
	if !strings.Contains(withoutAnchor, "refs/remotes/origin/main") {
		t.Fatalf("message without an anchor =\n%s\nwant it to name the publication branch's remote-tracking ref", withoutAnchor)
	}
	if strings.Contains(withoutAnchor, "refs/remotes/origin/HEAD") {
		t.Fatalf("message without an anchor =\n%s\nnames refs/remotes/origin/HEAD, which the private clone never has", withoutAnchor)
	}

	// A workspace publishing to master must be told about master.
	onMaster := pushRewrittenMessage(base, "", "/repo/.git/sanho/canonical", "master")
	if !strings.Contains(onMaster, "refs/remotes/origin/master") {
		t.Fatalf("message on a master workspace =\n%s\nwant it to name refs/remotes/origin/master", onMaster)
	}
}

func TestShortOIDAppliesTheHygieneWidth(t *testing.T) {
	tests := map[string]string{
		"":    "(none)",
		"abc": "abc",
		"67c4bbfeada37f5dda8fb79aa43216ef062cd8df": "67c4bbfeada3",
	}
	for input, want := range tests {
		if got := shortOID(input); got != want {
			t.Errorf("shortOID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHumanizeAge(t *testing.T) {
	tests := []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "less than a minute"},
		{time.Minute, "1 minute"},
		{90 * time.Minute, "1 hour"},
		{3 * time.Hour, "3 hours"},
		{50 * time.Hour, "2 days"},
	}
	for _, test := range tests {
		if got := humanizeAge(test.age); got != test.want {
			t.Errorf("humanizeAge(%v) = %q, want %q", test.age, got, test.want)
		}
	}
}

// §5.6: data older than 24 hours appends the age to the warning.
func TestStaleDataSuffix(t *testing.T) {
	got := commitBehindClean(1) + staleDataSuffix(50*time.Hour)
	want := "sanho: docs base is 1 commits behind — 'sanho sync' will merge cleanly (canonical last checked 2 days ago)"
	if got != want {
		t.Fatalf("stale warning =\n%q\nwant\n%q", got, want)
	}
}

// §5.9 message hygiene: English only. The only non-ASCII characters the
// catalog may carry are the em dash and right arrow the normative
// templates themselves use (audit L4).
func TestMessagesAreEnglishOnly(t *testing.T) {
	messages := []string{
		msgMigrateRequired,
		msgNotInWorkspace,
		msgSyncInProgressPush,
		msgMarkersBeforePush,
		msgCanonicalUnreachableAction,
		msgPushRejectedTrailer,
		msgCleanNeedsConfirmation,
		msgCleanSyncInProgress,
		msgInitNoProvenance,
		msgInitForceNeedsConfirmation,
		msgInitCanonicalEmpty,
		msgAlreadyMigrated,
		msgMigrateBlockedByTransaction,
		msgMigrateNeedsURL,
		neverFetchedLine,
		doctorFixHint,
		commitBehindClean(1),
		commitBehindConflicts(1, []string{"docs/a.md"}),
		commitBehindUnknown(1),
		syncConflictMessage("docs", []string{"docs/a.md"}),
		pushConflictMessage("a", "b", []string{"docs/a.md"}),
		pushSyncRequiredMessage("no_base", "a", "b"),
		pushSyncRequiredMessage(publish.ReasonUncorroboratedBase, sampleHeadOID, sampleHeadOID),
		pushMarkersMessage([]string{"docs/a.md"}),
		pushUnreachableMessage("git@host:docs.git", "connection refused"),
		pushRewrittenMessage("a", "b", "/clone", "main"),
		pushPublishedMessage("abc", "fast_forward"),
		syncedMessage("abc", "def"),
		upToDateMessage("abc"),
		upToDateMessage(""),
		pulledMessage("abc", ""),
		syncAbortedMessage(nil),
		syncCompletedMessage("abc", 0),
		syncCompletedMessage("abc", 3),
		syncContinueForeignHistoryMessage("it began at abc, and HEAD is def"),
		baseClearedMessage(),
		baseNotAdvancedMessage("the base file is read-only"),
		syncNeedsContinueMessage("a", "b", true),
		syncNeedsContinueMessage("a", "b", false),
		syncContinueBlockedMessage("markers remain"),
		baseRederivedMessage("abc"),
		stagedMarkersMessage([]string{"docs/a.md"}),
		unresolvedSyncMessage("docs", []string{"docs/a.md"}),
		syncNotCommittedMessage("a", "b"),
		syncNoteCorruptMessage("unexpected end of JSON input"),
		commitMsgStampWarning("no base"),
		staleCanonicalLine(time.Hour),
		registryLockHint("/home/u/.sanho/state.lock"),
	}
	messages = append(messages, daemonStopInstructions...)

	// U+2014 EM DASH and U+2192 RIGHTWARDS ARROW appear verbatim in the
	// §5.9 templates, so they are the permitted exceptions.
	const allowed = "—→"
	for _, message := range messages {
		for _, r := range message {
			if r < 128 || strings.ContainsRune(allowed, r) {
				continue
			}
			t.Errorf("message %q carries non-English character %q (U+%04X)", message, r, r)
		}
	}
}

// Every message that names a sanho command must name one that exists;
// P5's closure suite runs them, this pins the vocabulary.
func TestAdvisedCommandsAreRealCommands(t *testing.T) {
	known := map[string]bool{
		"sanho init": true, "sanho status": true, "sanho state": true,
		"sanho sync": true, "sanho pull": true, "sanho clean": true,
		"sanho doctor": true, "sanho project": true, "sanho hook": true,
		"sanho migrate": true, "sanho version": true,
	}

	// Messages whose next step is a *command*. Some guidance names a
	// flag instead (rerun with -y, rerun with --force); those are not
	// listed, because there is no command in them to check.
	advising := []string{
		msgMigrateRequired,
		msgNotInWorkspace,
		msgCleanNeedsConfirmation,
		msgCleanSyncInProgress,
		doctorFixHint,
		commitBehindClean(1),
		syncConflictMessage("docs", []string{"docs/a.md"}),
		pushConflictMessage("a", "b", []string{"docs/a.md"}),
		syncNotCommittedMessage("a", "b"),
		syncNeedsContinueMessage("a", "b", true),
		syncContinueBlockedMessage("markers remain"),
		syncNoteCorruptMessage("unexpected end of JSON input"),
		staleCanonicalLine(time.Hour),
		neverFetchedLine,
	}
	for _, message := range advising {
		found := false
		for command := range known {
			if strings.Contains(message, command) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("message %q advises no known sanho command", message)
		}
	}
}
