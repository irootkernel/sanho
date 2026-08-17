package cli

// `sanho project add|delete` (docs/cli-json.md): registry
// administration, file-based. Same UX as v0.1, no daemon.

import (
	"errors"
	"fmt"

	"github.com/irootkernel/sanho/internal/infra/registry"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage project registrations",
		RunE:  runGroup,
	}
	cmd.AddCommand(newProjectAddCmd(), newProjectDeleteCmd())
	return cmd
}

func newProjectAddCmd() *cobra.Command {
	var docsRepoURL string

	cmd := &cobra.Command{
		Use:   "add <project>",
		Short: "Register a project and its canonical docs repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if docsRepoURL == "" {
				return fmt.Errorf("--docs-repo-url is required")
			}
			return runProjectAdd(cmd, args[0], docsRepoURL)
		},
	}
	cmd.Flags().StringVar(&docsRepoURL, "docs-repo-url", "", "Canonical docs repository URL")
	return cmd
}

func runProjectAdd(cmd *cobra.Command, project, url string) error {
	file, err := openRegistry()
	if err != nil {
		return err
	}
	// upsertProject is the URL-conflict guard: registering a name that
	// already points somewhere else would let two workspaces publish to
	// different repositories under one project (audit M9).
	if err := updateRegistry(cmd.Context(), file, func(state *registry.State) error {
		return upsertProject(state, project, url)
	}); err != nil {
		return err
	}

	writef(cmd.OutOrStdout(), "sanho: registered project %s -> %s\n", project, url)
	return nil
}

func newProjectDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <project>",
		Short: "Remove a project registration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectDelete(cmd, args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Delete even while workspaces still reference the project")
	return cmd
}

// runProjectDelete removes a project registration.
//
// It refuses while workspaces still reference the project, because those
// workspaces would keep working from their own configs while `sanho
// state` stopped being able to explain where their docs go. --force is
// the deliberate override; the workspace entries are left alone either
// way, since deleting them would erase observations the checkouts
// themselves can still refresh.
func runProjectDelete(cmd *cobra.Command, project string, force bool) error {
	file, err := openRegistry()
	if err != nil {
		return err
	}

	var referencing []string
	if err := updateRegistry(cmd.Context(), file, func(state *registry.State) error {
		if _, ok := state.Projects[project]; !ok {
			return fmt.Errorf("project %q is not registered", project)
		}
		for _, workspace := range projectWorkspaces(*state, project) {
			referencing = append(referencing, workspace.LocalPath)
		}
		if len(referencing) > 0 && !force {
			return errors.New(projectHasWorkspacesMessage(project, len(referencing), referencing[0]))
		}
		delete(state.Projects, project)
		return nil
	}); err != nil {
		return err
	}

	writef(cmd.OutOrStdout(), "sanho: removed project %s\n", project)
	return nil
}
