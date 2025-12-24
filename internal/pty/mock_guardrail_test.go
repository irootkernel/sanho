package pty

import "github.com/SeventeenthEarth/kkachi/internal/domain/guardrail"

type mockGuardrail struct {
	validateFunc func(command string) guardrail.Result
}

func (m *mockGuardrail) Validate(command string) guardrail.Result {
	if m.validateFunc != nil {
		return m.validateFunc(command)
	}
	return guardrail.Result{Blocked: false}
}
