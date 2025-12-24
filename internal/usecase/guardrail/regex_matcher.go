package guardrail

import (
	"fmt"
	"regexp"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/guardrail"
)

// RegexMatcher implements guardrail.Guardrail using regex patterns.
type RegexMatcher struct {
	rules []compiledRule
}

type compiledRule struct {
	re     *regexp.Regexp
	reason string
}

// NewRegexMatcher creates a new RegexMatcher from the given rules.
func NewRegexMatcher(rules []config.SecurityRule) (*RegexMatcher, error) {
	compiledRules := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern %q: %w", rule.Pattern, err)
		}
		compiledRules = append(compiledRules, compiledRule{
			re:     re,
			reason: rule.Reason,
		})
	}
	return &RegexMatcher{rules: compiledRules}, nil
}

// Validate checks the command against all regex rules.
func (m *RegexMatcher) Validate(command string) guardrail.Result {
	for _, rule := range m.rules {
		if rule.re.MatchString(command) {
			return guardrail.Result{
				Blocked: true,
				Reason:  rule.reason,
			}
		}
	}
	return guardrail.Result{Blocked: false}
}
