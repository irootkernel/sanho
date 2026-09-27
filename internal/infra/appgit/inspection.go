package appgit

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// ErrExternalFilterConfigured is an availability limit, not a dirty-docs verdict.
var ErrExternalFilterConfigured = errors.New("read-only inspection cannot assess this Git filter configuration")

type inspectionPolicy struct {
	mu            sync.Mutex
	configuration map[string]string
}

// NewInspection binds local readers without constructing a canonical clone or
// changing user configuration. Filter admission is deferred until a reached
// worktree read, so note and marker diagnoses keep their normal precedence.
func NewInspection(ctx context.Context, workDir, docsDir string) (*Repo, error) {
	runner, err := gitx.NewInspection(ctx, workDir)
	if err != nil {
		return nil, err
	}
	r := New(workDir, docsDir, runner)
	r.inspection = &inspectionPolicy{}
	return r, nil
}

func (r *Repo) admitWorktreeRead(ctx context.Context, run *gitx.Runner) error {
	if r.inspection == nil {
		return nil
	}
	r.inspection.mu.Lock()
	defer r.inspection.mu.Unlock()
	res, err := r.git.Run(ctx, "config", "--null", "--list", "--includes")
	if err != nil {
		return fmt.Errorf("appgit: read inspection configuration: %w", err)
	}
	configuration := make(map[string]string)
	for _, record := range strings.Split(string(res.Stdout), "\x00") {
		if record == "" {
			continue
		}
		key, value, _ := strings.Cut(record, "\n")
		configuration[key] = value
	}
	if r.inspection.configuration != nil && !maps.Equal(r.inspection.configuration, configuration) {
		return fmt.Errorf("%w: effective configuration changed during inspection", gitx.ErrInspectionPolicyUnavailable)
	}
	for key, command := range configuration {
		if strings.HasPrefix(key, "filter.") &&
			(strings.HasSuffix(key, ".clean") || strings.HasSuffix(key, ".process")) && command != "" {
			return ErrExternalFilterConfigured
		}
	}
	r.inspection.configuration = configuration
	// Status can enter a submodule and invoke its independently configured
	// filters even with submodule.recurse=false. Admit neither the real nor
	// scratch index when that nested execution policy cannot be established.
	entries, err := run.Run(ctx, "ls-files", "--stage", "-z", "--", ":(literal)"+r.docsDir)
	if err != nil {
		return fmt.Errorf("appgit: read inspection index: %w", err)
	}
	for _, entry := range strings.Split(string(entries.Stdout), "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			return fmt.Errorf("%w: docs contain a submodule", gitx.ErrInspectionPolicyUnavailable)
		}
	}
	return nil
}

// docsSubtree distinguishes a genuinely absent path from unreadable tree objects.
// Inspect one exact entry at each level; no failure is converted to absence.
func (r *Repo) docsSubtree(ctx context.Context, root string) (string, error) {
	tree := root
	run := r.git.WithOptions(gitx.WithLocalObjects())
	for _, component := range strings.Split(r.docsDir, "/") {
		res, err := run.Run(ctx, "ls-tree", "-z", tree, "--", ":(literal)"+component)
		if err != nil {
			return "", fmt.Errorf("appgit: read docs subtree: %w", err)
		}
		if len(res.Stdout) == 0 {
			return r.EmptyTree(ctx)
		}
		entry, err := parseLsTreeEntry(strings.TrimSuffix(string(res.Stdout), "\x00"))
		if err != nil || entry.path != component || entry.kind != "tree" {
			return "", fmt.Errorf("appgit: docs path is not a readable tree")
		}
		tree = entry.object
	}
	if err := r.verifyDocsTree(ctx, tree); err != nil {
		return "", err
	}
	return tree, nil
}

// verifyDocsTree checks every required local tree and blob, including when two
// equal tree IDs would otherwise short-circuit a comparison. Gitlinks refer to
// separate repositories and do not require a local commit object.
func (r *Repo) verifyDocsTree(ctx context.Context, tree string) error {
	run := r.git.WithOptions(gitx.WithLocalObjects())
	res, err := run.Run(ctx, "ls-tree", "-r", "-z", tree)
	if err != nil {
		return fmt.Errorf("appgit: read required docs tree: %w", err)
	}
	var objects strings.Builder
	for _, record := range strings.Split(string(res.Stdout), "\x00") {
		if record == "" {
			continue
		}
		entry, err := parseLsTreeEntry(record)
		if err != nil {
			return errors.New("appgit: invalid docs tree entry")
		}
		if entry.kind == "blob" {
			objects.WriteString(entry.object + "\n")
		}
	}
	if objects.Len() == 0 {
		return nil
	}
	checked, err := run.RunWithStdin(ctx, strings.NewReader(objects.String()), "cat-file", "--batch-check")
	if err != nil {
		return fmt.Errorf("appgit: read required docs objects: %w", err)
	}
	var expectedBytes int64
	for _, line := range strings.Split(strings.TrimSuffix(string(checked.Stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[1] != "blob" {
			return errors.New("appgit: a required docs blob is missing or invalid")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return errors.New("appgit: invalid required docs blob size")
		}
		expectedBytes += int64(len(line)) + 1 + size + 1
	}
	// Reading headers alone cannot detect a damaged compressed blob body.
	// Drain content through Git with bounded capture rather than retain it.
	reader := run.WithOptions(gitx.WithStdoutLimit(1))
	contents, err := reader.RunWithStdin(ctx, strings.NewReader(objects.String()), "cat-file", "--batch")
	if err != nil {
		return fmt.Errorf("appgit: read required docs blob contents: %w", err)
	}
	if contents.StdoutBytes != expectedBytes {
		return errors.New("appgit: incomplete required docs blob contents")
	}
	return nil
}
