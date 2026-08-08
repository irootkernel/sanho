package cli

import (
	"context"

	"github.com/irootkernel/sanho/internal/infra/canonical"
)

// canonicalPort wraps canonical.Link for the publication port.
//
// It exists for one reason: the canonical repository is docs-only, so
// the merge it runs reports conflicted paths relative to *its* root,
// which is the docs root. The app-side merge already prefixes those with
// the workspace's docs directory (see appPort.MergeDocs), and the guidance contract's
// templates print `docs/api.md`. Prefixing here too means a conflict
// reads identically whether it was discovered at push time or at sync
// time, which is what lets one message template serve both.
type canonicalPort struct {
	*canonical.Link
	docsDir string
}

func (c canonicalPort) MergeDocs(ctx context.Context, baseCommit, oursTree, theirsCommit string) (string, []string, bool, error) {
	tree, conflicts, clean, err := c.Link.MergeDocs(ctx, baseCommit, oursTree, theirsCommit)
	if err != nil {
		return "", nil, false, err
	}
	return tree, prefixDocsPaths(c.docsDir, conflicts), clean, nil
}

func (c canonicalPort) MergeDocsTrees(ctx context.Context, baseTree, oursTree, theirsTree string) (string, []string, bool, error) {
	tree, conflicts, clean, err := c.Link.MergeDocsTrees(ctx, baseTree, oursTree, theirsTree)
	if err != nil {
		return "", nil, false, err
	}
	return tree, prefixDocsPaths(c.docsDir, conflicts), clean, nil
}

func (w *workspace) canonicalPort(store *canonical.Store) canonicalPort {
	return canonicalPort{Link: w.link(store), docsDir: w.config.DocsDir}
}
