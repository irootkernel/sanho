package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/infra/gitx"

	"github.com/spf13/cobra"
)

type diffOptions struct {
	refresh  bool
	local    bool
	stat     bool
	nameOnly bool
}

func newDiffCmd() *cobra.Command {
	var opts diffOptions
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Inspect incoming or local docs changes",
		Long: `Compare the recorded docs base with the cached canonical head.

Use --refresh to fetch canonical first, or --local to compare the base with
the application HEAD. Output paths are relative to the configured docs root.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.refresh, "refresh", false, "Fetch the canonical repository before comparing")
	cmd.Flags().BoolVar(&opts.local, "local", false, "Compare the recorded base with application HEAD")
	cmd.Flags().BoolVar(&opts.stat, "stat", false, "Print a diffstat instead of a patch")
	cmd.Flags().BoolVar(&opts.nameOnly, "name-only", false, "Print only changed paths")
	return cmd
}

func (o diffOptions) validate() error {
	switch {
	case o.stat && o.nameOnly:
		return errors.New("--stat and --name-only cannot be combined")
	case o.local && o.refresh:
		return errors.New("--local and --refresh cannot be combined")
	default:
		return nil
	}
}

func runDiff(cmd *cobra.Command, opts diffOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return err
	}
	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil {
		return err
	}
	if !hasBase {
		return errors.New("no docs base is recorded")
	}

	store, err := ws.openCanonical()
	if err != nil {
		return err
	}
	if opts.refresh {
		if err := store.Fetch(ctx); err != nil {
			return fmt.Errorf("refresh canonical repository: %w", err)
		}
	}

	fromTree, toTree := base.Tree, ""
	runner := gitx.New(store.Dir())
	if opts.local {
		toTree, err = ws.repo.HeadDocsTree(ctx)
		if err != nil {
			return fmt.Errorf("read the docs tree of HEAD: %w", err)
		}
		objects := filepath.Join(store.Dir(), "objects")
		runner = gitx.New(ws.root, gitx.WithEnv("GIT_ALTERNATE_OBJECT_DIRECTORIES="+objects))
	} else {
		_, toTree, err = store.Head(ctx)
		if errors.Is(err, pubdom.ErrEmptyBranch) {
			return errors.New("canonical repository has no commits to compare")
		}
		if err != nil {
			return err
		}
	}

	args := []string{"--no-pager", "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color"}
	switch {
	case opts.stat:
		args = append(args, "--stat")
	case opts.nameOnly:
		args = append(args, "--name-only")
	default:
		args = append(args, "--src-prefix=a/", "--dst-prefix=b/")
	}
	args = append(args, fromTree, toTree, "--")
	result, err := runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("compare docs trees: %w", err)
	}
	writef(cmd.OutOrStdout(), "%s", result.Stdout)
	return nil
}
