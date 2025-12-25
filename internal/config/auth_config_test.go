package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadAuthConfigFromEnv(t *testing.T) {
	t.Run("Default values", func(t *testing.T) {
		os.Unsetenv("AUTH_ENABLED")
		os.Unsetenv("AUTH_TOKEN")

		config := LoadAuthConfigFromEnv()

		assert.False(t, config.AuthEnabled)
		assert.Equal(t, "", config.AuthToken)
	})

	t.Run("Auth enabled with token", func(t *testing.T) {
		os.Setenv("AUTH_ENABLED", "true")
		os.Setenv("AUTH_TOKEN", "secret-token")
		defer os.Unsetenv("AUTH_ENABLED")
		defer os.Unsetenv("AUTH_TOKEN")

		config := LoadAuthConfigFromEnv()

		assert.True(t, config.AuthEnabled)
		assert.Equal(t, "secret-token", config.AuthToken)
	})

	t.Run("Auth disabled explicitly", func(t *testing.T) {
		os.Setenv("AUTH_ENABLED", "false")
		defer os.Unsetenv("AUTH_ENABLED")

		config := LoadAuthConfigFromEnv()

		assert.False(t, config.AuthEnabled)
	})
}
