package cli

// Shared helpers: JSON output, workspace guards, small formatting, and
// the argument checks the read-only canonical readers share.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
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

// normalizeDocsPath validates the `--path` a canonical reader was given
// and returns the cleaned, docs-root-relative form.
//
// Canonical is docs-only, so a docs-root-relative path is already a
// canonical one and needs no translation — only this containment check,
// and for one concrete reason: the value goes on to become a git
// pathspec or object spec, and a malformed one fails deep inside git
// with an error about something else.
//
// It is shared rather than duplicated because `sanho log --path` and
// `sanho show --path` name the same thing, and two copies of a
// containment check are two chances to disagree about what is inside
// the docs directory.
func normalizeDocsPath(value string) (string, error) {
	cleaned := path.Clean(filepath.ToSlash(strings.TrimRight(value, "/")))
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("%w: --path %q names the docs root; name a document", errInvalidArguments, value)
	}
	if !filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", fmt.Errorf("%w: --path %q must be inside the docs directory (no leading '/', no '..')",
			errInvalidArguments, value)
	}
	return cleaned, nil
}

// requireNonEmptyFlags refuses a narrowing flag that was handed an empty
// value.
//
// Absent and empty are the same value once the flag has been parsed into
// an options struct, and reading "given as empty" as "not given" would
// answer a narrowed question with the whole listing. That is not a
// theoretical shape: it is what an agent interpolating an unset variable
// produces, and the answer it would then attribute to a repository it
// never named.
func requireNonEmptyFlags(cmd *cobra.Command, names ...string) error {
	for _, name := range names {
		flag := cmd.Flags().Lookup(name)
		if flag != nil && flag.Changed && flag.Value.String() == "" {
			return fmt.Errorf("%w: --%s needs a value", errInvalidArguments, name)
		}
	}
	return nil
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
