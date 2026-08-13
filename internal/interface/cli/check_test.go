package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/usecase/admin"
)

type checkLocal struct {
	tree string
	err  error
}

func (l checkLocal) HeadDocsTree(context.Context) (string, error) { return l.tree, l.err }

type checkCanonical struct {
	resolved    bool
	resolveErr  error
	behind      int
	ahead       int
	distanceErr error
}

func (c checkCanonical) ResolveCommit(context.Context, string) (bool, error) {
	return c.resolved, c.resolveErr
}

func (c checkCanonical) Distance(context.Context, string, string) (int, int, error) {
	return c.behind, c.ahead, c.distanceErr
}

func TestBuildCheckJSONUsesAndSemantics(t *testing.T) {
	report := admin.StatusReport{
		WorkingCopyKnown:   true,
		DocsClean:          true,
		HasBase:            true,
		RelationKnown:      true,
		PublicationKnown:   true,
		PublicationPending: true,
	}
	document := buildCheckJSON(report, checkOptions{
		requireClean: true, requireCurrent: true, requirePublished: true,
	})
	if document.Passed {
		t.Fatal("Passed = true with a pending publication")
	}
	want := []policyCheckResultJSON{
		{Name: "clean", Passed: true, Reason: "clean"},
		{Name: "current", Passed: true, Reason: "current"},
		{Name: "published", Reason: "publication_pending"},
	}
	if !reflect.DeepEqual(document.Checks, want) {
		t.Fatalf("checks = %+v, want %+v", document.Checks, want)
	}
}

func TestCurrentCheckAcceptsAnEmptyCanonicalWithoutABase(t *testing.T) {
	result := currentCheck(admin.StatusReport{CanonicalEmpty: true})
	if !result.Passed || result.Reason != "canonical_empty" {
		t.Fatalf("current check = %+v, want canonical_empty pass", result)
	}
}

func TestDetectCheckPublicationPropagatesEvaluationFailure(t *testing.T) {
	wantErr := errors.New("head docs tree failed")
	_, _, err := detectCheckPublication(
		context.Background(),
		checkLocal{err: wantErr},
		provenance.Base{Tree: "base-tree"},
		true,
		false,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestDetectCheckPublicationSkipsComparisonWithoutStableBase(t *testing.T) {
	wantErr := errors.New("must not inspect local tree")
	for _, test := range []struct {
		name           string
		hasBase        bool
		syncInProgress bool
	}{
		{name: "no base"},
		{name: "sync in progress", hasBase: true, syncInProgress: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			known, pending, err := detectCheckPublication(
				context.Background(),
				checkLocal{err: wantErr},
				provenance.Base{Tree: "base-tree"},
				test.hasBase,
				test.syncInProgress,
			)
			if err != nil || known || pending {
				t.Fatalf("publication = (%t, %t, %v), want (false, false, nil)", known, pending, err)
			}
		})
	}
}

func TestDetectCheckRelationPropagatesEvaluationFailures(t *testing.T) {
	resolveErr := errors.New("resolve failed")
	distanceErr := errors.New("distance failed")
	for _, test := range []struct {
		name      string
		canonical checkCanonical
		wantErr   error
	}{
		{name: "resolve", canonical: checkCanonical{resolveErr: resolveErr}, wantErr: resolveErr},
		{name: "distance", canonical: checkCanonical{resolved: true, distanceErr: distanceErr}, wantErr: distanceErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := detectCheckRelation(context.Background(), test.canonical, "base", "head")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestDetectCheckRelationKeepsMissingCommitAsPolicyResult(t *testing.T) {
	known, behind, ahead, err := detectCheckRelation(context.Background(), checkCanonical{}, "base", "head")
	if err != nil || known || behind != 0 || ahead != 0 {
		t.Fatalf("relation = (%t, %d, %d, %v), want (false, 0, 0, nil)", known, behind, ahead, err)
	}
}

func TestCloneMissingHasActionableMachineCode(t *testing.T) {
	err := newCloneMissingError("/tmp/canonical")
	if got := machineErrorCode(err); got != codeCloneMissing {
		t.Fatalf("machine code = %q, want %q", got, codeCloneMissing)
	}
	if !errors.Is(err, errCloneMissing) {
		t.Fatal("clone missing sentinel was not preserved")
	}
}
