// Package markers implements v0.2 conflict-marker detection over raw
// content (docs/architecture.md "Merge and marker contracts").
//
// Pure over bytes: callers (infra/cli) read blobs or files — applying
// MaxScanSize — and pass content here. This replaces the v0.1 detector,
// whose three silent failure modes (swallowed read errors, 64 KiB line
// blindness, binary false positives) were audit finding H2.
package markers

import (
	"bytes"
	"errors"
)

// Marker byte sequences at line starts.
const (
	Start  = "<<<<<<< "
	Middle = "======="
	End    = ">>>>>>> "
)

// MaxScanSize bounds how much content a caller may pass. Files larger
// than this must be reported as errors by the caller (never silently
// passed): ErrTooLarge is the shared sentinel.
const MaxScanSize = 10 << 20 // 10 MiB

// ErrTooLarge is returned by callers' read helpers when content exceeds
// MaxScanSize. Gates treat it as a failure, not as "no markers".
var ErrTooLarge = errors.New("content too large to scan for conflict markers")

// Result of scanning one file's content.
type Result struct {
	// Binary is true when the content sniffs as binary (NUL byte within
	// the first 8 KiB); binary content is never reported as conflicted.
	Binary bool
	// HasMarkers is true when all three marker sequences occur at line
	// starts, in order (Start before Middle before End).
	HasMarkers bool
}

// Scan classifies content. It never errors: size policy is enforced by
// the caller via MaxScanSize/ErrTooLarge, and unreadable files are the
// caller's error to propagate (fail closed).
func Scan(content []byte) Result {
	if isBinary(content) {
		return Result{Binary: true, HasMarkers: false}
	}
	return Result{HasMarkers: hasOrderedMarkers(content)}
}

// BinarySniffSize is how much of the leading content is inspected for a
// NUL byte when classifying binary content. The "Merge and marker
// contracts" section of docs/architecture.md fixes the window at 8 KiB.
//
// It is exported because callers need it: the merge contract's ordering is sniff
// first, size second (F-M8), so a file over MaxScanSize still has to be
// classified — and reading exactly this many bytes is what lets that
// happen without materializing a gigabyte.
const BinarySniffSize = 8 << 10 // 8 KiB

// isBinary reports a NUL byte within the first 8 KiB of content (or the
// whole content, if shorter).
func isBinary(content []byte) bool {
	n := len(content)
	if n > BinarySniffSize {
		n = BinarySniffSize
	}
	return bytes.IndexByte(content[:n], 0) >= 0
}

// hasOrderedMarkers reports whether content contains a Start line, then
// (later) a Middle line, then (later still) an End line, each anchored
// at a line start. It is a single left-to-right scan: because each
// phase resumes exactly where the previous one left off, finding the
// earliest matching line for each phase in turn is sufficient to detect
// any valid Start-then-Middle-then-End ordering, however much unrelated
// content (including decoy markers in the wrong order) surrounds them.
func hasOrderedMarkers(content []byte) bool {
	afterStart, ok := scanForLine(content, 0, matchesStart)
	if !ok {
		return false
	}
	afterMiddle, ok := scanForLine(content, afterStart, matchesMiddle)
	if !ok {
		return false
	}
	_, ok = scanForLine(content, afterMiddle, matchesEnd)
	return ok
}

// lineSpan identifies one line within content. [start, end) is the line
// without its terminating '\n' (a trailing '\r' from a CRLF ending is
// included in this range); next is the start offset of the following
// line. Once content is exhausted, next is len(content)+1, which is
// always past scanForLine's loop bound, so scanning stops cleanly.
type lineSpan struct {
	start, end, next int
}

// lineAt returns the line beginning at the line-start offset start.
// Unlike bufio.Scanner, this has no maximum line length: a single very
// long line never hides markers that follow it later in content (v0.1's
// audit finding H2 64 KiB blindness).
func lineAt(content []byte, start int) lineSpan {
	if rel := bytes.IndexByte(content[start:], '\n'); rel >= 0 {
		end := start + rel
		return lineSpan{start: start, end: end, next: end + 1}
	}
	return lineSpan{start: start, end: len(content), next: len(content) + 1}
}

// scanForLine walks lines starting at the line-start offset from and
// returns the offset immediately after the first line satisfying match,
// so the caller can resume scanning for the next marker there.
func scanForLine(content []byte, from int, match func([]byte, lineSpan) bool) (after int, ok bool) {
	for start := from; start <= len(content); {
		line := lineAt(content, start)
		if match(content, line) {
			return line.next, true
		}
		start = line.next
	}
	return 0, false
}

// matchesStart reports a line beginning with the Start prefix. The rest
// of the line (the ref label) is unconstrained.
func matchesStart(content []byte, line lineSpan) bool {
	return bytes.HasPrefix(content[line.start:line.end], []byte(Start))
}

// matchesEnd reports a line beginning with the End prefix. The rest of
// the line (the ref label) is unconstrained.
func matchesEnd(content []byte, line lineSpan) bool {
	return bytes.HasPrefix(content[line.start:line.end], []byte(End))
}

// matchesMiddle requires the line to be exactly "=======", tolerating a
// trailing '\r' from a CRLF ending but nothing else: git emits exactly
// seven '=' alone on the line, so neither "==========" nor
// "=======label" (both of which a substring/prefix check would wrongly
// accept) may match.
func matchesMiddle(content []byte, line lineSpan) bool {
	end := line.end
	if end > line.start && content[end-1] == '\r' {
		end--
	}
	return bytes.Equal(content[line.start:end], []byte(Middle))
}
