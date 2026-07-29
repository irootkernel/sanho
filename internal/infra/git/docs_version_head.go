package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

type HeadDocsVersion struct {
	AppCommit string
	DocsHash  docs.CommitHash
}

// ResolveHeadDocsVersion finds the newest reachable docs-version commit whose
// docs tree is identical to HEAD. Later non-doc commits are therefore allowed,
// while unmanaged docs changes are rejected.
func (c *Client) ResolveHeadDocsVersion(
	ctx context.Context,
	repoPath, docsDir string,
) (HeadDocsVersion, bool, error) {
	cmd := exec.CommandContext(
		ctx,
		"git",
		"-C",
		repoPath,
		"log",
		"--format=%H",
		"--max-count=100",
		"--grep=^docs-version:",
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && len(strings.TrimSpace(string(out))) == 0 {
			return HeadDocsVersion{}, false, nil
		}
		return HeadDocsVersion{}, false, fmt.Errorf("list docs-version commits: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	for _, commit := range strings.Fields(string(out)) {
		messageOut, err := runWorkspaceGit(ctx, repoPath, nil, "show", "-s", "--format=%B", commit)
		if err != nil {
			return HeadDocsVersion{}, false, err
		}
		hash := docsVersionFromMessage(string(messageOut))
		if hash.IsZero() {
			continue
		}
		diff := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet", commit, "HEAD", "--", docsDir)
		diff.Env = os.Environ()
		switch err := diff.Run(); {
		case err == nil:
			return HeadDocsVersion{AppCommit: commit, DocsHash: hash}, true, nil
		case isExitCode(err, 1):
			continue
		case err != nil:
			return HeadDocsVersion{}, false, fmt.Errorf("compare docs-version commit %s with HEAD: %w", commit, err)
		}
	}
	return HeadDocsVersion{}, false, nil
}

func docsVersionFromMessage(message string) docs.CommitHash {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "docs-version:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "docs-version:"))
		if value != "" {
			return docs.CommitHash(value)
		}
	}
	return ""
}

func isExitCode(err error, code int) bool {
	exitErr, ok := err.(*exec.ExitError)
	return ok && exitErr.ExitCode() == code
}
