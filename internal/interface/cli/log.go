package cli

// `sanho log` — canonical history and the reverse traceability the
// publication commit convention has always recorded.
//
// Every canonical commit Sanho writes carries `source: <workspace-id> @
// <app tip>` and the app subjects behind it, expressly so the change can
// be traced back to the repository that made it. Nothing read it. This
// command is that reader: it decodes what publication wrote, answers
// from the cached clone, and writes nothing anywhere.
//
// It deliberately does NOT require a recorded base, unlike `sanho diff`.
// The state this command matters most in — canonical history rewritten,
// no anchor the base can be re-derived from — is exactly the state where
// the base is unusable, and a listing that refused there would be
// useless in the one place the guidance names it.

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/infra/canonical"

	"github.com/spf13/cobra"
)

// defaultLogCount bounds the default listing. History is unbounded and
// the terminal is not; `--max-count` raises it.
const defaultLogCount = 20

// Entry kinds. Canonical history is not all Sanho's: docs writers commit
// into the canonical repository directly, and those commits are ordinary
// history rather than a malformed publication.
const (
	logKindPublication = "publication"
	logKindExternal    = "external"
)

type logOptions struct {
	refresh  bool
	asJSON   bool
	maxCount int
	docsPath string
}

type logJSON struct {
	Branch         string         `json:"branch"`
	FetchedEver    bool           `json:"fetched_ever"`
	DataAgeSeconds int64          `json:"data_age_seconds"`
	Entries        []logEntryJSON `json:"entries"`
}

type logEntryJSON struct {
	Commit      string `json:"commit"`
	Tree        string `json:"tree"`
	CommittedAt string `json:"committed_at"`
	Subject     string `json:"subject"`
	Kind        string `json:"kind"`
	// Source is null for an external commit: the field is absent
	// information, not an empty record.
	Source              *logSourceJSON `json:"source"`
	ApplicationSubjects []string       `json:"application_subjects"`
}

type logSourceJSON struct {
	Repository        string `json:"repository"`
	Branch            string `json:"branch"`
	WorkspaceID       string `json:"workspace_id"`
	ApplicationCommit string `json:"application_commit"`
}

func newLogCmd() *cobra.Command {
	var opts logOptions
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show canonical docs history and where each change came from",
		Long: `List canonical commits, newest first.

For each commit Sanho published, the publication provenance is decoded:
the application repository, branch, workspace, and application commit the
docs change came from. Commits made directly in the canonical repository
are listed as ordinary history.

Use --refresh to fetch canonical first, and --path to narrow to one
document. Paths are relative to the configured docs root, the same way
sanho diff reports them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLog(cmd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.refresh, "refresh", false, "Fetch the canonical repository before reading")
	cmd.Flags().IntVarP(&opts.maxCount, "max-count", "n", defaultLogCount, "Maximum number of commits to list")
	cmd.Flags().StringVar(&opts.docsPath, "path", "", "Limit to commits touching this docs path")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

// normalize validates the flags and returns the canonical path to filter
// on, which normalizeDocsPath checks for containment on every reader's
// behalf.
func (o logOptions) normalize() (string, error) {
	if o.maxCount < 1 {
		return "", fmt.Errorf("%w: --max-count must be at least 1", errInvalidArguments)
	}
	if o.docsPath == "" {
		return "", nil
	}
	return normalizeDocsPath(o.docsPath)
}

func runLog(cmd *cobra.Command, opts logOptions) error {
	docsPath, err := opts.normalize()
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	store, err := ws.openCanonical()
	if err != nil {
		// Read-only commands never create the clone; a write path does.
		return finishCommand(cmd, nil, opts.asJSON, newCloneMissingError(ws.cloneDir()))
	}
	if opts.refresh {
		if err := store.Fetch(ctx); err != nil {
			return finishCommand(cmd, nil, opts.asJSON, fmt.Errorf("refresh canonical repository: %w", err))
		}
	}

	entries, err := store.Log(ctx, opts.maxCount, docsPath)
	// A canonical repository nothing has published into yet has no
	// history, which is an answer rather than a failure — the same
	// reading `sanho status` gives it with canonical.empty.
	if err != nil && !errors.Is(err, pubdom.ErrEmptyBranch) {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	age, fetchedEver := store.Age()
	document := buildLogJSON(store.Branch(), age, fetchedEver, entries)
	if opts.asJSON {
		return writeJSON(cmd.OutOrStdout(), document)
	}
	renderLog(cmd.OutOrStdout(), document, docsPath)
	writeln(cmd.ErrOrStderr(), dataAgeLine(age, fetchedEver))
	return nil
}

// buildLogJSON takes the canonical facts rather than the store so the
// mapping that decides publication-vs-external can be driven through
// both kinds without a real clone.
func buildLogJSON(branch string, age time.Duration, fetchedEver bool, entries []canonical.LogEntry) logJSON {
	document := logJSON{
		Branch:      branch,
		FetchedEver: fetchedEver,
		Entries:     []logEntryJSON{},
	}
	if fetchedEver {
		document.DataAgeSeconds = int64(age.Seconds())
	}

	for _, entry := range entries {
		row := logEntryJSON{
			Commit:              entry.Commit,
			Tree:                entry.Tree,
			CommittedAt:         entry.CommittedAt.UTC().Format(time.RFC3339),
			Subject:             messageSubject(entry.Message),
			Kind:                logKindExternal,
			ApplicationSubjects: []string{},
		}
		if meta, ok := pubdom.ParseCommitMeta(entry.Message); ok {
			row.Kind = logKindPublication
			row.Source = &logSourceJSON{
				Repository:        meta.RepoName,
				Branch:            meta.Branch,
				WorkspaceID:       meta.WorkspaceID,
				ApplicationCommit: meta.TipOID,
			}
			row.ApplicationSubjects = orEmpty(meta.Subjects)
		}
		document.Entries = append(document.Entries, row)
	}
	return document
}

// messageSubject takes a commit message's first line. Trailing content
// is the body, which the publication rows report through Source instead.
func messageSubject(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return strings.TrimRight(line, "\r")
}

func renderLog(out io.Writer, document logJSON, docsPath string) {
	if len(document.Entries) == 0 {
		if docsPath != "" {
			writef(out, "no canonical commits touch %s\n", docsPath)
			return
		}
		writeln(out, "canonical has no commits yet")
		return
	}

	// Continuation lines align under the subject, past the OID column.
	indent := strings.Repeat(" ", shortOIDWidth)
	for _, entry := range document.Entries {
		// RFC3339 separates the date from the time with "T".
		date, _, _ := strings.Cut(entry.CommittedAt, "T")
		writef(out, "%s  %s  %s\n", shortOID(entry.Commit), date, entry.Subject)
		if entry.Source == nil {
			continue
		}
		writef(out, "%s  from %s @ %s\n", indent,
			entry.Source.WorkspaceID, shortOID(entry.Source.ApplicationCommit))
		for _, subject := range entry.ApplicationSubjects {
			writef(out, "%s    - %s\n", indent, subject)
		}
	}
}
