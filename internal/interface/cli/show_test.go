package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/canonical"
)

func TestShowOptionsRejectUnusablePaths(t *testing.T) {
	tests := map[string]string{
		"absolute path":    "/etc/passwd",
		"escaping path":    "../secrets.md",
		"docs root itself": ".",
		"bare separator":   "/",
	}
	for name, docsPath := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := showOptions{docsPath: docsPath}.normalize()
			if err == nil {
				t.Fatalf("normalize(--path %q) succeeded, want an error", docsPath)
			}
			// The refusal is the caller's argument, so the --json envelope
			// owes invalid_arguments rather than a sanho defect.
			if !errors.Is(err, errInvalidArguments) {
				t.Errorf("normalize(--path %q) error = %v, want errInvalidArguments", docsPath, err)
			}
		})
	}
}

func TestShowOptionsNormalizeAcceptedPaths(t *testing.T) {
	tests := map[string]struct {
		in   string
		want string
	}{
		"no document":       {in: "", want: ""},
		"plain document":    {in: "api.md", want: "api.md"},
		"nested document":   {in: "guides/api.md", want: "guides/api.md"},
		"trailing slash":    {in: "guides/", want: "guides"},
		"redundant segment": {in: "guides/./api.md", want: "guides/api.md"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := showOptions{docsPath: test.in}.normalize()
			if err != nil {
				t.Fatalf("normalize(--path %q) error = %v", test.in, err)
			}
			if got != test.want {
				t.Errorf("normalize(--path %q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// TestBuildShowListingJSONKeepsOneShape pins the half of the document
// that a listing does not fill: an agent must be able to read `path` and
// `document` as absent records rather than having to know which mode it
// asked for.
func TestBuildShowListingJSONKeepsOneShape(t *testing.T) {
	document := buildShowListingJSON(
		"9a41f2c0e1d2c3b4a5968778695a4b3c2d1e0f9a",
		"1111111111111111111111111111111111111111",
		[]canonical.TreeEntry{
			{Path: "api.md", Mode: "100644", OID: "2222222222222222222222222222222222222222", Size: 42},
			{Path: "guides/setup.md", Mode: "100644", OID: "3333333333333333333333333333333333333333", Size: 7},
		})

	if document.Path != nil {
		t.Errorf("path = %q, want null in listing mode", *document.Path)
	}
	if document.Document != nil {
		t.Errorf("document = %+v, want null in listing mode", document.Document)
	}
	if len(document.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(document.Entries))
	}
	if document.Entries[1].Path != "guides/setup.md" || document.Entries[1].Size != 7 {
		t.Errorf("second entry = %+v, want the nested document with its size", document.Entries[1])
	}
	// Machine OIDs are full length (the JSON contract).
	if len(document.Entries[0].OID) != 40 {
		t.Errorf("entry OID = %q, want the full OID", document.Entries[0].OID)
	}
}

func TestBuildShowDocumentJSONReportsTextAndBinaryDistinctly(t *testing.T) {
	const commit = "9a41f2c0e1d2c3b4a5968778695a4b3c2d1e0f9a"
	const tree = "1111111111111111111111111111111111111111"

	text := buildShowDocumentJSON(commit, tree, canonical.Document{
		Path: "api.md", OID: "2222222222222222222222222222222222222222",
		Size: 9, Content: []byte("line one\n"),
	})
	if text.Path == nil || *text.Path != "api.md" {
		t.Fatalf("path = %v, want api.md", text.Path)
	}
	if text.Document == nil {
		t.Fatal("document = null for a text document")
	}
	if text.Document.Binary {
		t.Error("binary = true for a text document")
	}
	if text.Document.Content == nil || *text.Document.Content != "line one\n" {
		t.Errorf("content = %v, want the document's bytes", text.Document.Content)
	}
	// The JSON contract: an empty collection is [], never null.
	if text.Entries == nil {
		t.Error("entries is nil, want an empty array")
	}

	binary := buildShowDocumentJSON(commit, tree, canonical.Document{
		Path: "logo.png", OID: "4444444444444444444444444444444444444444",
		Size: 4096, Binary: true,
	})
	if binary.Document == nil {
		t.Fatal("document = null for a binary document")
	}
	if !binary.Document.Binary {
		t.Error("binary = false for a binary document")
	}
	// Absent content, not empty content: the difference is what tells an
	// agent it has the whole answer.
	if binary.Document.Content != nil {
		t.Errorf("content = %q, want null for a binary document", *binary.Document.Content)
	}
	if binary.Document.Size != 4096 {
		t.Errorf("size = %d, want the byte count even with no content", binary.Document.Size)
	}
}

// TestShowRefusalsCarryTheirMachineCode is the mistake lockTimeoutError's
// comment records, checked for the two refusals `sanho show` composes:
// rewording must not cost the code an agent branches on.
func TestShowRefusalsCarryTheirMachineCode(t *testing.T) {
	unknownCommit := newUnknownTargetError(showUnknownCommitMessage("deadbeef"))
	if got := machineErrorCode(unknownCommit); got != codeUnknownTarget {
		t.Errorf("machineErrorCode(unknown commit) = %q, want %q", got, codeUnknownTarget)
	}
	if !strings.Contains(unknownCommit.Error(), "--refresh") {
		t.Errorf("unknown-commit refusal = %q, want it to offer --refresh", unknownCommit)
	}

	unknownDoc := newUnknownTargetError(showUnknownDocumentMessage(
		"9a41f2c0e1d2c3b4a5968778695a4b3c2d1e0f9a", "missing.md"))
	if got := machineErrorCode(unknownDoc); got != codeUnknownTarget {
		t.Errorf("machineErrorCode(unknown document) = %q, want %q", got, codeUnknownTarget)
	}
	// Human output shortens OIDs; the machine document does not.
	if !strings.Contains(unknownDoc.Error(), "9a41f2c0e1d2") ||
		strings.Contains(unknownDoc.Error(), "9a41f2c0e1d2c") {
		t.Errorf("unknown-document refusal = %q, want a 12-character OID", unknownDoc)
	}

	tooLarge := newTooLargeError(showTooLargeMessage("huge.md", markers.MaxScanSize+1))
	if got := machineErrorCode(tooLarge); got != codeTooLarge {
		t.Errorf("machineErrorCode(too large) = %q, want %q", got, codeTooLarge)
	}
}
