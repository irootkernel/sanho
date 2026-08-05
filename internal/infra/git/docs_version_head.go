package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

type HeadDocsVersion struct {
	AppCommit string
	DocsHash  docs.CommitHash
}

// DocsVersionCandidate is the newest reachable commit with docs-version
// trailers whose docs tree matches a proposed application tip.
type DocsVersionCandidate struct {
	AppCommit     string
	TrailerValues []string
}

// ResolveHeadDocsVersion finds the newest reachable docs-version commit whose
// docs tree is identical to HEAD. Later non-doc commits are therefore allowed,
// while unmanaged docs changes are rejected.
func (c *Client) ResolveHeadDocsVersion(
	ctx context.Context,
	repoPath, docsDir string,
) (HeadDocsVersion, bool, error) {
	candidate, found, err := c.ResolveDocsVersionCandidate(ctx, repoPath, "HEAD", docsDir)
	if err != nil || !found {
		return HeadDocsVersion{}, found, err
	}
	if len(candidate.TrailerValues) != 1 || candidate.TrailerValues[0] == "" {
		return HeadDocsVersion{}, false, nil
	}
	return HeadDocsVersion{
		AppCommit: candidate.AppCommit,
		DocsHash:  docs.CommitHash(candidate.TrailerValues[0]),
	}, true, nil
}

// ResolveDocsVersionCandidate finds the newest reachable commit with at least
// one docs-version trailer whose docs tree is identical to tip. It deliberately
// returns raw trailer values so callers can reject malformed or duplicate
// provenance instead of falling back to older metadata.
func (c *Client) ResolveDocsVersionCandidate(
	ctx context.Context,
	repoPath, tip, docsDir string,
) (DocsVersionCandidate, bool, error) {
	resolvedTip, err := c.ResolveCommit(ctx, repoPath, tip)
	if err != nil {
		return DocsVersionCandidate{}, false, fmt.Errorf("resolve application tip: %w", err)
	}
	cmd := exec.CommandContext(
		ctx,
		"git",
		"-C",
		repoPath,
		"log",
		resolvedTip,
		"--format=%H",
		"--grep=docs-version",
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && len(strings.TrimSpace(string(out))) == 0 {
			return DocsVersionCandidate{}, false, nil
		}
		return DocsVersionCandidate{}, false, fmt.Errorf("list docs-version commits: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	for _, commit := range strings.Fields(string(out)) {
		messageOut, err := runWorkspaceGit(ctx, repoPath, nil, "show", "-s", "--format=%B", commit)
		if err != nil {
			return DocsVersionCandidate{}, false, err
		}
		values, err := docsVersionTrailerValues(ctx, string(messageOut))
		if err != nil {
			return DocsVersionCandidate{}, false, err
		}
		if len(values) == 0 {
			continue
		}
		diff := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet", commit, resolvedTip, "--", docsDir)
		diff.Env = os.Environ()
		switch err := diff.Run(); {
		case err == nil:
			return DocsVersionCandidate{AppCommit: commit, TrailerValues: values}, true, nil
		case isExitCode(err, 1):
			continue
		case err != nil:
			return DocsVersionCandidate{}, false, fmt.Errorf("compare docs-version commit %s with application tip: %w", commit, err)
		}
	}
	return DocsVersionCandidate{}, false, nil
}

func docsVersionTrailerValues(ctx context.Context, message string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "interpret-trailers", "--parse")
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("parse commit trailers: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	values := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "docs-version" {
			continue
		}
		values = append(values, strings.TrimSpace(value))
	}
	return values, nil
}

func isExitCode(err error, code int) bool {
	exitErr, ok := err.(*exec.ExitError)
	return ok && exitErr.ExitCode() == code
}
