package cli

import (
	"reflect"
	"testing"

	"github.com/irootkernel/sanho/internal/usecase/admin"
)

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
