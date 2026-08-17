package cli

// `sanho show` — what one canonical commit publishes, and what a
// document said at it.
//
// It is the reader docs/recovery.md used to have to describe in raw git.
// Rewrite recovery asks the user to choose a `--rebase-onto` anchor;
// `sanho log` lists the candidates and decodes the provenance of the
// ones sanho published — and a commit made directly in the canonical
// repository carries no provenance to decode, so only its CONTENT can
// settle whether it is the right anchor. That inspection was the last
// piece of guidance reaching past sanho's own surface, spelled as a
// `git -C <clone-dir> ls-tree -r` against a private clone the user had
// to locate first.
//
// Like `sanho log`, and for its reason, it requires no recorded base:
// the state it matters most in is precisely the state where no base
// resolves. It writes no application ref, index, worktree, base, or
// registry state, and opens no network without `--refresh`.

import (
	"errors"
	"fmt"
	"io"

	"github.com/irootkernel/sanho/internal/infra/canonical"

	"github.com/spf13/cobra"
)

type showOptions struct {
	refresh  bool
	asJSON   bool
	docsPath string
}

// showJSON is one document for both modes rather than two shapes to
// tell apart. `path` and `document` are null in listing mode and
// `entries` is empty in document mode, which is the JSON contract's own
// rule applied here: an absent record is null, an empty collection is
// `[]`, and neither is invented from the other.
type showJSON struct {
	Commit   string          `json:"commit"`
	Tree     string          `json:"tree"`
	Path     *string         `json:"path"`
	Entries  []showEntryJSON `json:"entries"`
	Document *showDocJSON    `json:"document"`
}

type showEntryJSON struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

// showDocJSON carries one document's content. Content is null exactly
// when Binary is true: sanho classifies what it reads and never returns
// bytes it has not, so "this is a 40 KiB image" is the complete answer
// rather than a truncated one.
type showDocJSON struct {
	OID     string  `json:"oid"`
	Size    int64   `json:"size"`
	Binary  bool    `json:"binary"`
	Content *string `json:"content"`
}

func newShowCmd() *cobra.Command {
	var opts showOptions
	cmd := &cobra.Command{
		Use:   "show <commit>",
		Short: "Show what a canonical commit publishes",
		Long: `List the documents one canonical commit publishes.

The commit is any revision the canonical clone resolves: a full or
abbreviated OID, or a ref such as refs/remotes/origin/main — the same
values sanho sync --rebase-onto accepts, so a rewrite-recovery candidate
can be inspected before it is adopted.

Use --path to print one document as of that commit instead of listing
them, and --refresh to fetch canonical first. Paths are relative to the
configured docs root, the same way sanho diff and sanho log report them.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd, opts, args[0])
		},
	}
	cmd.Flags().BoolVar(&opts.refresh, "refresh", false, "Fetch the canonical repository before reading")
	cmd.Flags().StringVar(&opts.docsPath, "path", "", "Print this document instead of listing the commit")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func (o showOptions) normalize() (string, error) {
	if o.docsPath == "" {
		return "", nil
	}
	return normalizeDocsPath(o.docsPath)
}

func runShow(cmd *cobra.Command, opts showOptions, rev string) error {
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

	commit, tree, err := store.ResolveDocsCommit(ctx, rev)
	if errors.Is(err, canonical.ErrUnknownObject) {
		return finishCommand(cmd, nil, opts.asJSON, newUnknownTargetError(showUnknownCommitMessage(rev)))
	}
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	if docsPath == "" {
		return showListing(cmd, opts, store, commit, tree)
	}
	return showDocument(cmd, opts, store, commit, tree, docsPath)
}

func showListing(cmd *cobra.Command, opts showOptions, store *canonical.Store, commit, tree string) error {
	entries, err := store.ListTree(cmd.Context(), commit)
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	document := buildShowListingJSON(commit, tree, entries)
	if opts.asJSON {
		return writeJSON(cmd.OutOrStdout(), document)
	}
	renderShowListing(cmd.OutOrStdout(), commit, document.Entries)
	return nil
}

func showDocument(cmd *cobra.Command, opts showOptions, store *canonical.Store, commit, tree, docsPath string) error {
	found, err := store.ReadDocument(cmd.Context(), commit, docsPath)
	switch {
	case errors.Is(err, canonical.ErrUnknownObject):
		return finishCommand(cmd, nil, opts.asJSON,
			newUnknownTargetError(showUnknownDocumentMessage(commit, docsPath)))
	case err != nil:
		var tooLarge *canonical.TooLargeError
		if errors.As(err, &tooLarge) {
			return finishCommand(cmd, nil, opts.asJSON,
				newTooLargeError(showTooLargeMessage(tooLarge.Path, tooLarge.Size)))
		}
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	if opts.asJSON {
		return writeJSON(cmd.OutOrStdout(), buildShowDocumentJSON(commit, tree, found))
	}
	// Binary content is a complete answer, not a failure, so the note
	// goes to stderr and stdout stays exactly the document's bytes —
	// which is what makes a redirect of the text case correct.
	if found.Binary {
		writeln(cmd.ErrOrStderr(), showBinaryDocumentMessage(found.Path, found.Size))
		return nil
	}
	writef(cmd.OutOrStdout(), "%s", found.Content)
	return nil
}

func buildShowListingJSON(commit, tree string, entries []canonical.TreeEntry) showJSON {
	document := showJSON{Commit: commit, Tree: tree, Entries: []showEntryJSON{}}
	for _, entry := range entries {
		document.Entries = append(document.Entries, showEntryJSON{
			Path: entry.Path,
			Mode: entry.Mode,
			OID:  entry.OID,
			Size: entry.Size,
		})
	}
	return document
}

func buildShowDocumentJSON(commit, tree string, found canonical.Document) showJSON {
	path := found.Path
	body := &showDocJSON{OID: found.OID, Size: found.Size, Binary: found.Binary}
	if !found.Binary {
		content := string(found.Content)
		body.Content = &content
	}
	return showJSON{
		Commit:   commit,
		Tree:     tree,
		Path:     &path,
		Entries:  []showEntryJSON{},
		Document: body,
	}
}

// renderShowListing prints one docs-root-relative path per line, which
// is the form the listing is used in: read by eye, and piped into grep
// or a loop. Mode, OID and size are facts about an object rather than
// about a document, so they stay in the JSON document.
func renderShowListing(out io.Writer, commit string, entries []showEntryJSON) {
	if len(entries) == 0 {
		writef(out, "canonical commit %s publishes no documents\n", shortOID(commit))
		return
	}
	for _, entry := range entries {
		writeln(out, entry.Path)
	}
}
