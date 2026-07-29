package git

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var ErrUnknownCommit = errors.New("unknown_commit")
var ErrNoDocsVersionCommits = errors.New("no_docs_version_commits")

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Clone(ctx context.Context, repoURL, path string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w\n%s", err, string(output))
	}
	return nil
}

// ConfigUser sets git user.email and user.name for a repository.
// This is needed for commits to work in environments without global git config.
func (c *Client) ConfigUser(ctx context.Context, path, email, name string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "user.email", email)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.email failed: %w\n%s", err, string(output))
	}
	cmd = exec.CommandContext(ctx, "git", "-C", path, "config", "user.name", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.name failed: %w\n%s", err, string(output))
	}
	return nil
}

func (c *Client) Fetch(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "fetch")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w\n%s", err, string(output))
	}
	return nil
}

func (c *Client) Pull(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "pull", "--ff-only")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull failed: %w\n%s", err, string(output))
	}
	return nil
}

func (c *Client) RevParseHead(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// VerifyCommit checks whether the given commit-ish exists in the repository.
func (c *Client) VerifyCommit(ctx context.Context, path string, commit string) error {
	// cat-file -e exits 0 if the object exists and is the right type.
	cmd := exec.CommandContext(ctx, "git", "-C", path, "cat-file", "-e", commit+"^{commit}")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return ErrUnknownCommit
		}
		return fmt.Errorf("failed to run git to verify commit: %w", err)
	}
	return nil
}

// ResolveCommit resolves a commit-ish to a full commit hash to avoid injection.
func (c *Client) ResolveCommit(ctx context.Context, path string, commit string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--verify", commit+"^{commit}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", ErrUnknownCommit
		}
		return "", fmt.Errorf("failed to run git rev-parse to resolve commit: %w\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// CompareCommits returns the number of commits reachable only from left and
// only from right. Both arguments must already be resolved commit hashes.
func (c *Client) CompareCommits(ctx context.Context, path, left, right string) (int, int, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-list", "--left-right", "--count", left+"..."+right)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list failed: %w\n%s", err, string(out))
	}

	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected git rev-list output: %q", strings.TrimSpace(string(out)))
	}
	ahead, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead count %q: %w", fields[0], err)
	}
	behind, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind count %q: %w", fields[1], err)
	}
	return ahead, behind, nil
}

func (c *Client) ArchiveDocs(ctx context.Context, path string, commit string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)

	// Archive the entire repository at the given commit. We intentionally do
	// not restrict this to a specific subdirectory so that the docs snapshot
	// can reflect the full docs repo layout. Consumers (CLI) are responsible
	// for mapping the archive root into their local docs directory and
	// ignoring files they don't need (e.g., dotfiles).
	cmd := exec.CommandContext(ctx, "git", "-C", path, "archive", "--format=tar", commit)
	cmd.Stdout = gw

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	closeErr := gw.Close()

	if runErr != nil {
		if strings.Contains(stderr.String(), "did not match any files") {
			if closeErr != nil {
				return nil, fmt.Errorf("failed to close gzip writer for empty archive: %w", closeErr)
			}
			return buf.Bytes(), nil
		}
		if closeErr != nil {
			return nil, fmt.Errorf("git archive failed: %w (stderr: %s); closing gzip writer failed: %v", runErr, stderr.String(), closeErr)
		}
		return nil, fmt.Errorf("git archive failed: %w, stderr: %s", runErr, stderr.String())
	}

	if closeErr != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", closeErr)
	}

	return buf.Bytes(), nil
}

// CheckoutMain checks out the main branch.
func (c *Client) CheckoutMain(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "checkout", "main")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try master if main doesn't exist
		cmd2 := exec.CommandContext(ctx, "git", "-C", path, "checkout", "master")
		if output2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("git checkout main/master failed: main: %v, master: %v\n%s\n%s", err, err2, string(output), string(output2))
		}
	}
	return nil
}

// ResetHardToOriginMain resets the working directory to origin/main.
func (c *Client) ResetHardToOriginMain(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "reset", "--hard", "origin/main")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try origin/master if origin/main doesn't exist
		cmd2 := exec.CommandContext(ctx, "git", "-C", path, "reset", "--hard", "origin/master")
		if output2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("git reset --hard origin/main or origin/master failed: main: %v, master: %v\n%s\n%s", err, err2, string(output), string(output2))
		}
	}
	return nil
}

// DiffIsEmpty checks if there are any changes in the working directory.
func (c *Client) DiffIsEmpty(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "diff", "--quiet")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Non-zero exit means there are differences
			return false, nil
		}
		return false, fmt.Errorf("git diff failed: %w", err)
	}
	return true, nil
}

// DiffStagedIsEmpty checks if there are any staged changes.
func (c *Client) DiffStagedIsEmpty(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Non-zero exit means there are staged differences
			return false, nil
		}
		return false, fmt.Errorf("git diff --cached failed: %w", err)
	}
	return true, nil
}

// AddDocs stages all relevant changes in the docs repository.
// In the current design, the docs repo root is treated as the docs tree,
// so we stage all changes under the repository path.
func (c *Client) AddDocs(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "add", ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add . failed: %w\n%s", err, string(output))
	}
	return nil
}

// Commit creates a new commit with the given message.
func (c *Client) Commit(ctx context.Context, path, message, authorEmail string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "commit", "-m", message, "--author", fmt.Sprintf("Sanho User <%s>", authorEmail))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w\n%s", err, string(output))
	}
	return nil
}

// Push pushes the current branch to origin.
func (c *Client) Push(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "push", "origin", "HEAD")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push failed: %w\n%s", err, string(output))
	}
	return nil
}

// HasDocsVersionCommits reports whether any reachable commit message contains a docs-version tag.
func (c *Client) HasDocsVersionCommits(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "log", "--grep", "^docs-version:", "--format=%H", "-n", "1")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Empty repo (no commits) returns non-zero; treat as no matches.
			if exitErr.ExitCode() != 0 {
				return false, nil
			}
		}
		return false, fmt.Errorf("git log for docs-version failed: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// GetLastDocsVersionHash returns the most recent docs-version hash from commit messages reachable from HEAD.
func (c *Client) GetLastDocsVersionHash(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "log", "--grep", "^docs-version:", "--format=%B", "-n", "1")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() != 0 {
				return "", ErrNoDocsVersionCommits
			}
		}
		return "", fmt.Errorf("git log for docs-version failed: %w", err)
	}
	msg := string(out)
	if strings.TrimSpace(msg) == "" {
		return "", ErrNoDocsVersionCommits
	}
	lines := strings.Split(msg, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "docs-version:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			hash := strings.TrimSpace(parts[1])
			if hash == "" {
				continue
			}
			return hash, nil
		}
	}
	return "", ErrNoDocsVersionCommits
}

// IsPathClean reports whether the given path has no staged or unstaged changes.
// path may be a directory or file; it must be relative to the repo root.
func (c *Client) IsPathClean(ctx context.Context, repoPath, path string) (bool, error) {
	// Unstaged changes
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet", "--", path)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git diff --quiet -- %s failed: %w", path, err)
	}

	// Staged changes
	cmd = exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--quiet", "--", path)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git diff --cached --quiet -- %s failed: %w", path, err)
	}

	// Untracked files
	statusCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain", "--", path)
	out, err := statusCmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain -- %s failed: %w", path, err)
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return false, nil
	}

	return true, nil
}
