// Package guardrail provides security policies for PTY sessions.
package guardrail

// Result represents the outcome of a guardrail check.
type Result struct {
	Blocked bool
	Reason  string
}

// Guardrail defines the interface for checking commands against security policies.
type Guardrail interface {
	// Validate checks if the given command line is allowed.
	Validate(command string) Result
}
