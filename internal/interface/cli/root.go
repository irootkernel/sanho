// Package cli provides the command-line interface for kkachi.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes following the roadmap's Phase 0 exit-code policy:
// - 0: Success
// - 1: User-fixable issues (environment, config, server/network errors)
// - 2+: Unexpected internal bugs
const (
	ExitCodeSuccess       = 0
	ExitCodeUserError     = 1
	ExitCodeInternalError = 2
)

// ErrInternal represents an unexpected internal error that should exit with code 2.
var ErrInternal = errors.New("internal error")

// BuildInfo contains build-time information injected via ldflags.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// verbose is a global flag for enabling debug output.
var verbose bool

// buildInfo stores build-time information.
var buildInfo BuildInfo

// NewRootCmd creates and returns the root command.
func NewRootCmd(info BuildInfo) *cobra.Command {
	buildInfo = info

	rootCmd := &cobra.Command{
		Use:   "kkachi",
		Short: "A document coordination system for Git repositories",
		Long: `Kkachi is a central document coordination system designed to synchronize
a specific documentation directory (e.g., docs/) across multiple Git repositories.

It ensures that documentation remains consistent and version-controlled
in a dedicated repository, separate from the application code.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	// Register subcommands
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newFixCmd())
	rootCmd.AddCommand(newHookCmd())
	rootCmd.AddCommand(newProjectCmd())
	rootCmd.AddCommand(newWorkspaceCmd())
	rootCmd.AddCommand(newStateCmd())

	return rootCmd
}

// Execute runs the root command.
func Execute(info BuildInfo) {
	cmd := NewRootCmd(info)
	if err := cmd.Execute(); err != nil {
		// Print the error message
		cmd.PrintErrln("Error:", err)

		// Determine exit code based on error type
		if errors.Is(err, ErrInternal) {
			os.Exit(ExitCodeInternalError)
		}
		os.Exit(ExitCodeUserError)
	}
}

// IsVerbose returns whether verbose mode is enabled.
func IsVerbose() bool {
	return verbose
}

// logDebug prints a debug message if verbose mode is enabled.
// This function should only be called after CLI initialization.
func logDebug(cmd *cobra.Command, format string, args ...interface{}) {
	if verbose && cmd != nil {
		cmd.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// LogDebugStderr prints a debug message to stderr if verbose mode is enabled.
// Note: verbose flag is set during Cobra flag parsing, so this function
// only produces output after rootCmd.Execute() has been called.
// For debugging before CLI initialization, consider using environment variables.
func LogDebugStderr(format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}
