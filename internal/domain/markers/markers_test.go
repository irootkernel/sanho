package markers_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/markers"
)

func TestScan(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    markers.Result
	}{
		{
			name: "true conflict fixture, as git merge-tree --write-tree emits it",
			content: []byte("shared preamble\n" +
				"<<<<<<< sanho-ours\n" +
				"our line\n" +
				"=======\n" +
				"their line\n" +
				">>>>>>> sanho-upstream\n" +
				"shared tail\n"),
			want: markers.Result{HasMarkers: true},
		},
		{
			name:    "order violation: end before start",
			content: []byte(">>>>>>> theirs\n=======\n<<<<<<< ours\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "order violation: middle before start",
			content: []byte("=======\n<<<<<<< ours\n>>>>>>> theirs\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "order violation: end appears before middle (start, end, middle)",
			content: []byte("<<<<<<< ours\n>>>>>>> theirs\n=======\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name: "a decoy end marker positioned before the genuine sequence does not block detection",
			content: []byte(">>>>>>> decoy\n" +
				"<<<<<<< ours\n=======\ntheirs\n>>>>>>> theirs\n"),
			want: markers.Result{HasMarkers: true},
		},
		{
			name:    "mid-line occurrences of all three sequences do not count",
			content: []byte("text <<<<<<< ours embedded\nrandom=======line\nmore >>>>>>> theirs text\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name: "binary content containing all three sequences is never reported as conflicted (audit false-positive case)",
			content: append([]byte("\x00binary garbage"),
				[]byte("\n<<<<<<< ours\n=======\n>>>>>>> theirs\n")...),
			want: markers.Result{Binary: true, HasMarkers: false},
		},
		{
			name:    "CRLF fixture: a trailing '\\r' does not break middle-line exactness",
			content: []byte("preamble\r\n<<<<<<< ours\r\nmine\r\n=======\r\ntheirs\r\n>>>>>>> theirs\r\ntail\r\n"),
			want:    markers.Result{HasMarkers: true},
		},
		{
			name:    "CRLF: eight equals plus CR is still not an exact middle line",
			content: []byte("<<<<<<< ours\r\n========\r\n>>>>>>> theirs\r\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "empty content",
			content: []byte{},
			want:    markers.Result{},
		},
		{
			name:    "nil content",
			content: nil,
			want:    markers.Result{},
		},
		{
			name:    "content that is exactly one marker: start only",
			content: []byte("<<<<<<< ours\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "content that is exactly one marker: middle only",
			content: []byte("=======\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "content that is exactly one marker: end only",
			content: []byte(">>>>>>> theirs\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "ten equals signs is not a middle marker (git emits exactly seven)",
			content: []byte("<<<<<<< ours\n==========\n>>>>>>> theirs\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "seven equals plus trailing text is not a middle marker",
			content: []byte("<<<<<<< ours\n=======theirs\n>>>>>>> theirs\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "start prefix without the trailing space does not match",
			content: []byte("<<<<<<<ours\n=======\n>>>>>>> theirs\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name:    "end prefix without the trailing space does not match",
			content: []byte("<<<<<<< ours\n=======\n>>>>>>>theirs\n"),
			want:    markers.Result{HasMarkers: false},
		},
		{
			name: "a single line over 64 KiB does not hide markers that follow it (v0.1 H2 blindness)",
			content: []byte(strings.Repeat("a", 70000) +
				"\n<<<<<<< ours\n=======\n>>>>>>> theirs\n"),
			want: markers.Result{HasMarkers: true},
		},
		{
			name:    "no trailing newline after the end marker still detects",
			content: []byte("<<<<<<< ours\nmine\n=======\ntheirs\n>>>>>>> theirs"),
			want:    markers.Result{HasMarkers: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markers.Scan(tt.content)
			if got != tt.want {
				t.Errorf("Scan(%d bytes) = %+v, want %+v", len(tt.content), got, tt.want)
			}
		})
	}
}

func TestScan_BinarySniffBoundary(t *testing.T) {
	const sniffSize = 8 << 10 // docs/architecture.md fixes the sniff window at 8 KiB.

	withNULAt := func(totalLen, nulIndex int) []byte {
		content := bytes.Repeat([]byte("a"), totalLen)
		content[nulIndex] = 0
		return content
	}

	tests := []struct {
		name    string
		content []byte
		want    bool // Result.Binary
	}{
		{
			name:    "NUL at the last byte of the first 8 KiB is sniffed",
			content: withNULAt(sniffSize+1000, sniffSize-1),
			want:    true,
		},
		{
			name:    "NUL just past the first 8 KiB boundary is not sniffed",
			content: withNULAt(sniffSize+1000, sniffSize),
			want:    false,
		},
		{
			name:    "NUL anywhere in content shorter than 8 KiB is sniffed",
			content: withNULAt(100, 99),
			want:    true,
		},
		{
			name:    "no NUL, content shorter than 8 KiB",
			content: bytes.Repeat([]byte("a"), 100),
			want:    false,
		},
		{
			name:    "no NUL, content longer than 8 KiB",
			content: bytes.Repeat([]byte("a"), sniffSize+1000),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markers.Scan(tt.content).Binary; got != tt.want {
				t.Errorf("Scan(...).Binary = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMaxScanSizeAndErrTooLarge is a compile-time and value proof that
// these two symbols exist, are exported, and match their documented
// contract: gate callers (infra/cli, outside this package) size bounded
// reads against MaxScanSize and classify oversized files by comparing
// the read error against ErrTooLarge.
func TestMaxScanSizeAndErrTooLarge(t *testing.T) {
	const wantMaxScanSize = 10 << 20 // 10 MiB, per the doc comment
	if markers.MaxScanSize != wantMaxScanSize {
		t.Errorf("MaxScanSize = %d, want %d", markers.MaxScanSize, wantMaxScanSize)
	}

	if markers.ErrTooLarge == nil {
		t.Fatal("ErrTooLarge must not be nil")
	}
	if markers.ErrTooLarge.Error() == "" {
		t.Error("ErrTooLarge must carry a non-empty message")
	}
	if !errors.Is(markers.ErrTooLarge, markers.ErrTooLarge) {
		t.Error("ErrTooLarge must be usable as an errors.Is sentinel")
	}
}

// TestMarkerConstants pins the exported byte sequences to their
// documented values, since the marker detector's correctness (and every
// documented marker example depends on their exact spelling.
func TestMarkerConstants(t *testing.T) {
	if markers.Start != "<<<<<<< " {
		t.Errorf("Start = %q, want %q", markers.Start, "<<<<<<< ")
	}
	if markers.Middle != "=======" {
		t.Errorf("Middle = %q, want %q", markers.Middle, "=======")
	}
	if markers.End != ">>>>>>> " {
		t.Errorf("End = %q, want %q", markers.End, ">>>>>>> ")
	}
}
