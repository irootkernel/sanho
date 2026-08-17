package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/usecase/publish"
)

func TestBuildPreviewJSONReportsADecidedVerdict(t *testing.T) {
	tests := map[string]struct {
		result        publish.Preview
		wantVerdict   string
		wantPublishes bool
	}{
		"fast forward": {
			result:        publish.Preview{Case: pubdom.CaseFastForward, Publishes: true, Head: sampleHeadOID},
			wantVerdict:   "fast_forward",
			wantPublishes: true,
		},
		"auto merge": {
			result:        publish.Preview{Case: pubdom.CaseAutoMerge, Publishes: true, Head: sampleHeadOID},
			wantVerdict:   "auto_merge",
			wantPublishes: true,
		},
		"up to date": {
			result:        publish.Preview{Case: pubdom.CaseUpToDate, Head: sampleHeadOID},
			wantVerdict:   "up_to_date",
			wantPublishes: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			document, ok := buildPreviewJSON("main", sampleBaseOID, test.result, nil)
			if !ok {
				t.Fatal("a decided verdict was reported as an evaluation failure")
			}
			if document.Verdict != test.wantVerdict {
				t.Errorf("verdict = %q, want %q", document.Verdict, test.wantVerdict)
			}
			if document.Publishes != test.wantPublishes {
				t.Errorf("publishes = %v, want %v", document.Publishes, test.wantPublishes)
			}
			if document.Blocked {
				t.Error("blocked = true for a verdict that would let the push through")
			}
			if document.Branch != "main" || document.Tip != sampleBaseOID {
				t.Errorf("document = %+v, want the previewed branch and tip", document)
			}
			// The JSON contract: an empty collection is [], never null.
			if document.Conflicts == nil {
				t.Error("conflicts is nil, want an empty array")
			}
		})
	}
}

// TestBuildPreviewJSONReportsARefusalAsAVerdict is the command's whole
// premise: a push that would be rejected is an answer, not a failure of
// the preview.
func TestBuildPreviewJSONReportsARefusalAsAVerdict(t *testing.T) {
	tests := map[string]struct {
		err           error
		wantVerdict   string
		wantConflicts []string
	}{
		"unfinished sync": {
			err:           publish.ErrSyncInProgress,
			wantVerdict:   previewSyncInProgress,
			wantConflicts: []string{},
		},
		"committed markers": {
			err:           &publish.MarkersPresentError{Tip: sampleBaseOID, Paths: []string{"docs/api.md"}},
			wantVerdict:   previewMarkersPresent,
			wantConflicts: []string{"docs/api.md"},
		},
		"conflicted merge": {
			err: &publish.SyncRequiredError{
				Base: sampleBaseOID, Head: sampleHeadOID,
				Conflicts: []string{"docs/api.md", "docs/schema.md"},
				Reason:    publish.ReasonConflicts,
			},
			wantVerdict:   previewSyncRequired,
			wantConflicts: []string{"docs/api.md", "docs/schema.md"},
		},
		"no recorded base": {
			err:           &publish.SyncRequiredError{Head: sampleHeadOID, Reason: publish.ReasonNoBase},
			wantVerdict:   previewSyncRequired,
			wantConflicts: []string{},
		},
		"rewritten history": {
			err:           publish.ErrHistoryRewritten,
			wantVerdict:   previewHistoryRewritten,
			wantConflicts: []string{},
		},
		// The machine-code table folds the empty-publication refusal into
		// the sync_required family; the verdicts must not, because the two
		// states call for entirely different actions.
		"empty publication": {
			err:           &publish.EmptyPublishError{Branch: "main", Head: sampleHeadOID, DocsCount: 7},
			wantVerdict:   previewEmptyPublication,
			wantConflicts: []string{},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			document, ok := buildPreviewJSON("main", sampleBaseOID, publish.Preview{}, test.err)
			if !ok {
				t.Fatal("a rejection was reported as an evaluation failure")
			}
			if document.Verdict != test.wantVerdict {
				t.Errorf("verdict = %q, want %q", document.Verdict, test.wantVerdict)
			}
			if !document.Blocked {
				t.Error("blocked = false for a push that would be rejected")
			}
			if document.Publishes {
				t.Error("publishes = true for a push that would be rejected")
			}
			if !reflect.DeepEqual(document.Conflicts, test.wantConflicts) {
				t.Errorf("conflicts = %v, want %v", document.Conflicts, test.wantConflicts)
			}
		})
	}
}

// TestBuildPreviewJSONSeparatesEvaluationFailures keeps the two kinds of
// bad news apart: "this push would be refused" is a verdict, while "the
// verdict could not be reached" is an error like any other.
func TestBuildPreviewJSONSeparatesEvaluationFailures(t *testing.T) {
	for name, err := range map[string]error{
		"canonical unreachable": pubdom.ErrUnreachable,
		"unclassified failure":  errors.New("resolve the empty tree: git exploded"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := buildPreviewJSON("main", sampleBaseOID, publish.Preview{}, err); ok {
				t.Errorf("%v was reported as a verdict, want an evaluation failure", err)
			}
		})
	}
}

// TestPreviewMessagesNameTheStateWithoutNamingACommand pins the boundary
// the command keeps: the push rejection owns the recovery, so preview
// describes the state in full and advises nothing.
func TestPreviewMessagesNameTheStateWithoutNamingACommand(t *testing.T) {
	verdicts := []string{
		previewSyncInProgress, previewMarkersPresent, previewSyncRequired,
		previewHistoryRewritten, previewEmptyPublication,
	}
	for _, verdict := range verdicts {
		message := previewBlockedMessage("main", verdict)
		if !strings.Contains(message, verdict) {
			t.Errorf("%q does not carry its machine verdict %q", message, verdict)
		}
		if !strings.Contains(message, "would be rejected") {
			t.Errorf("%q does not say the push would be rejected", message)
		}
		// A bare verdict name would mean the sentence never gained prose.
		if strings.Contains(message, "— "+verdict) {
			t.Errorf("%q describes the state only by its machine name", message)
		}
		if advisedSanhoCommand.MatchString(message) || advisedGitCommand.MatchString(message) {
			t.Errorf("%q names a next command; preview leaves guidance to the push boundary", message)
		}
	}

	publishing := previewOutcomeMessage("main", "fast_forward", true)
	if !strings.Contains(publishing, "fast_forward") || !strings.Contains(publishing, "would publish") {
		t.Errorf("%q does not report a publishing verdict", publishing)
	}
	nothing := previewOutcomeMessage("main", "up_to_date", false)
	if !strings.Contains(nothing, "would publish nothing") {
		t.Errorf("%q does not report that nothing would be published", nothing)
	}
}

func TestPreviewBranchRefusalsCarryTheirMachineCode(t *testing.T) {
	unknown := newUnknownTargetError(previewUnknownBranchMessage("nope"))
	if got := machineErrorCode(unknown); got != codeUnknownTarget {
		t.Errorf("machineErrorCode(unknown branch) = %q, want %q", got, codeUnknownTarget)
	}
	if !strings.Contains(previewDetachedHeadMessage(), "--branch") {
		t.Errorf("the detached-HEAD refusal does not offer --branch: %q", previewDetachedHeadMessage())
	}
}
