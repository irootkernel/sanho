package cli

// Shared helpers: JSON output, workspace guards, small formatting.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// writeJSON renders v as an indented JSON document with a trailing
// newline. It is only ever handed cmd.OutOrStdout(): the JSON contract requires that
// stdout under --json carry the document and nothing else, with every
// diagnostic on stderr, so a machine reader never has to strip prose.
func writeJSON(out io.Writer, v any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return errors.Join(errInternal, fmt.Errorf("render JSON output: %w", err))
	}
	return nil
}

// writeCompactJSON renders v without presentation whitespace and with a
// trailing newline. It is reserved for interfaces whose contract requires a
// compact document rather than the default indented form.
func writeCompactJSON(out io.Writer, v any) error {
	if err := json.NewEncoder(out).Encode(v); err != nil {
		return errors.Join(errInternal, fmt.Errorf("render JSON output: %w", err))
	}
	return nil
}

// orEmpty turns a nil slice into an empty one so JSON renders `[]`
// rather than `null`. A machine reader should not have to distinguish
// "no conflicts" from "absent".
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// requireV2Workspace resolves the workspace for a command that needs a
// migrated one. A v0.1 workspace is refused with the legacy-workspace contract hint, which
// names the only command that succeeds in that state.
func requireV2Workspace(ctx context.Context) (*workspace, error) {
	ws, err := openWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return ws, nil
}

// writef and writeln are the only way this package writes user-facing
// output.
//
// They drop the write error deliberately, and that is a decision rather
// than an oversight: a failed write to stdout or stderr cannot be
// reported (the channel for reporting it is the one that just failed)
// and cannot be acted on by the user. Funnelling every print through two
// functions keeps that judgment in one place instead of scattering
// `_, _ =` across every renderer, and keeps errcheck meaningful for the
// writes that do matter.
func writef(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}

func writeln(out io.Writer, args ...any) {
	_, _ = fmt.Fprintln(out, args...)
}
