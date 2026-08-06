// Package markers implements v0.2 conflict-marker detection over raw
// content (sanho-v0.2.md §5.4 "Marker detector").
//
// Pure over bytes: callers (infra/cli) read blobs or files — applying
// MaxScanSize — and pass content here. This replaces the v0.1 detector,
// whose three silent failure modes (swallowed read errors, 64 KiB line
// blindness, binary false positives) were audit finding H2.
package markers

import "errors"

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
	panic("unimplemented (sanho v0.2 P1)")
}
