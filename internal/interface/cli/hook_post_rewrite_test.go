package cli

import (
	"strings"
	"testing"
)

func TestReadGitRewriteMappings(t *testing.T) {
	mappings, err := readGitRewriteMappings(strings.NewReader(
		"aaaaaaaa bbbbbbbb\ncccccccc dddddddd extra\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 2 {
		t.Fatalf("mapping count=%d want 2", len(mappings))
	}
	if mappings[0].Old != "aaaaaaaa" || mappings[0].New != "bbbbbbbb" {
		t.Fatalf("first mapping=%+v", mappings[0])
	}
	if mappings[1].Old != "cccccccc" || mappings[1].New != "dddddddd" {
		t.Fatalf("second mapping=%+v", mappings[1])
	}
}

func TestReadGitRewriteMappingsRejectsMalformedInput(t *testing.T) {
	if _, err := readGitRewriteMappings(strings.NewReader("only-one-field\n")); err == nil {
		t.Fatal("malformed rewrite mapping was accepted")
	}
}
