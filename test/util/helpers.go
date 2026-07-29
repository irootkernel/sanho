package testutil

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// RunCmd executes a command with optional working directory and environment overrides.
func RunCmd(dir string, extraEnv map[string]string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	return cmd.CombinedOutput()
}

// WaitForHealth polls the given URL until it returns 200 or context cancels.
func WaitForHealth(ctx context.Context, url string) error {
	client := &http.Client{Timeout: 1 * time.Second}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil && resp.StatusCode == http.StatusOK {
				_ = resp.Body.Close()
				return nil
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
	}
}
