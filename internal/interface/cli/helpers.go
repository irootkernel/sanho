package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/config"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

// Default timeout for CLI operations.
const DefaultTimeout = 30 * time.Second

// LongTimeout is used for operations that may take longer (e.g., init with snapshot download).
const LongTimeout = 60 * time.Second

// stdinReader is the default reader for user input, can be replaced in tests.
var stdinReader = bufio.NewReader(os.Stdin)

// promptForInput reads a line from stdin and returns the trimmed string.
func promptForInput(prompt string) (string, error) {
	fmt.Print(prompt)
	input, err := stdinReader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(input), nil
}

// promptForEmail gets email from git config or prompts the user.
// Returns the email address or an error if not provided.
func promptForEmail(ctx context.Context, path string) (string, error) {
	detector := git.NewDetector()
	email, _ := detector.GetUserEmail(ctx, path)
	if email != "" {
		return email, nil
	}

	// If the context is already canceled, honor it before prompting.
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("prompt for email canceled or timed out: %w", ctx.Err())
		default:
		}
	}

	// Prompt for email if not found in git config, but respect the context deadline.
	type result struct {
		input string
		err   error
	}

	ch := make(chan result, 1)
	go func() {
		input, err := promptForInput("Enter your email address: ")
		ch <- result{input: input, err: err}
	}()

	var res result
	if ctx == nil {
		res = <-ch
	} else {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("prompt for email canceled or timed out: %w", ctx.Err())
		case res = <-ch:
		}
	}

	if res.err != nil {
		return "", fmt.Errorf("failed to read email: %w", res.err)
	}
	if res.input == "" {
		return "", errors.New("email address is required")
	}
	return res.input, nil
}

// promptForConfirmation asks the user for y/N confirmation.
// Returns true if confirmed, false if aborted.
// If aborted, it prints "Aborted." message.
func promptForConfirmation(prompt string) (bool, error) {
	input, err := promptForInput(prompt)
	if err != nil {
		return false, err
	}
	input = strings.ToLower(input)
	if input != "y" && input != "yes" {
		fmt.Println("Aborted.")
		return false, nil
	}
	return true, nil
}

// getWorkingDirectory returns the current working directory or an error.
func getWorkingDirectory() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return cwd, nil
}

// createContext creates a context with the specified timeout.
func createContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// validateRequiredFlag returns an error if the flag value is empty.
func validateRequiredFlag(name, value string) error {
	if value == "" {
		return fmt.Errorf("--%s is required", name)
	}
	return nil
}

func resolveSocketPath(configuredPath string) (string, error) {
	if socketPathFlag != "" {
		configuredPath = socketPathFlag
	}
	if configuredPath != "" {
		if !filepath.IsAbs(configuredPath) {
			return "", errors.New("sanho socket path must be an absolute path")
		}
		return filepath.Clean(configuredPath), nil
	}
	paths, err := config.ResolveRuntimePaths(os.Getenv("SANHO_HOME"), os.Getenv("SANHO_SOCKET"))
	if err != nil {
		return "", err
	}
	return paths.SocketPath, nil
}

func newDaemonClient(configuredPath string) (*httpclient.HTTPClient, error) {
	socketPath, err := resolveSocketPath(configuredPath)
	if err != nil {
		return nil, err
	}
	return httpclient.NewHTTPClient(socketPath), nil
}

// resolveDocsPath resolves docsDir against cwd and ensures that the resulting
// path stays within the workspace directory to avoid accidental deletion of
// arbitrary paths when using --force.
func resolveDocsPath(cwd, docsDir string) (string, error) {
	// Absolute docsDir is dangerous (e.g., --docs-dir /home/user).
	if filepath.IsAbs(docsDir) {
		return "", fmt.Errorf("docs directory '%s' must be a relative path within the workspace", docsDir)
	}

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	candidate := filepath.Join(absCwd, docsDir)
	absDocsPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve docs directory: %w", err)
	}

	rel, err := filepath.Rel(absCwd, absDocsPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative docs directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("docs directory '%s' must be inside the workspace", docsDir)
	}

	return absDocsPath, nil
}
