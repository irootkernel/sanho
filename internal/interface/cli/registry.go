package cli

// Registry access helpers (sanho-v0.2.md §5.7).
//
// The registry is observational: it answers "what other checkouts of
// this project exist and where were they last seen", and nothing about
// publication correctness depends on it (D4). Every write therefore goes
// through one function, so that a failure to record has exactly one
// place to be handled and the flow that actually succeeded is never
// undone by a bookkeeping error.

import (
	"context"
	"fmt"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/registry"
)

// upsertProject records a project's docs repository URL, refusing a
// conflicting one.
//
// The refusal names both URLs because that is the whole content of the
// problem: two workspaces disagreeing about where a project's docs live
// would publish to different repositories under one name. v0.1 keyed
// projects by URL *basename*, so two repositories named `docs.git` on
// different hosts silently overwrote each other (audit M9); full URLs
// plus this check are the replacement.
func upsertProject(state *registry.State, project, url string) error {
	existing, ok := state.Projects[project]
	if ok && existing.DocsRepoURL != "" && existing.DocsRepoURL != url {
		return fmt.Errorf("project %q is already registered with docs repository %s, not %s",
			project, existing.DocsRepoURL, url)
	}
	state.Projects[project] = registry.Project{DocsRepoURL: url}
	return nil
}

// upsertWorkspace refreshes this workspace's entry under the registry
// lock.
func upsertWorkspace(ctx context.Context, file *registry.File, ws *workspace, base provenance.Base) error {
	return file.Update(ctx, func(state *registry.State) error {
		if err := upsertProject(state, ws.config.Project, ws.config.DocsRepoURL); err != nil {
			return err
		}
		state.Workspaces[ws.registryKey()] = registry.Workspace{
			Project:       ws.config.Project,
			LocalPath:     ws.root,
			BaseCommit:    base.Commit,
			BaseTree:      base.Tree,
			ActorEmail:    ws.config.ActorEmail,
			LastUpdatedAt: time.Now().UTC(),
		}
		return nil
	})
}

// removeWorkspace drops this workspace's entry. The project registration
// is left alone: other workspaces may still reference it, and
// `sanho project delete` is the command that removes one deliberately.
func removeWorkspace(ctx context.Context, file *registry.File, key string) error {
	return file.Update(ctx, func(state *registry.State) error {
		delete(state.Workspaces, key)
		return nil
	})
}

// projectWorkspaces lists a project's registered workspaces. An empty
// project name lists every workspace.
func projectWorkspaces(state registry.State, project string) []registry.Workspace {
	var rows []registry.Workspace
	for _, workspace := range state.Workspaces {
		if project == "" || workspace.Project == project {
			rows = append(rows, workspace)
		}
	}
	return rows
}
