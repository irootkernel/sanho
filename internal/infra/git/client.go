package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

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

func (c *Client) RevParseHead(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
