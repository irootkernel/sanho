package git

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrUnknownCommit = errors.New("unknown_commit")

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

func (c *Client) ArchiveDocs(ctx context.Context, path string, commit string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)

	cmd := exec.CommandContext(ctx, "git", "-C", path, "archive", "--format=tar", commit, "docs/")
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
