package config

import (
	"os"
	"strconv"
)

type DocsRepoConfig struct {
	ID      string
	Path    string
	RepoURL string
}

type Config struct {
	DocsRepos []DocsRepoConfig
}

type AuthConfig struct {
	AuthEnabled bool
	AuthToken   string
}

// ResolveWebDistDir returns the path to the web distribution directory.
// Falls back to "web/dist" if envValue is empty.
func ResolveWebDistDir(envValue string) string {
	if envValue != "" {
		return envValue
	}
	return "web/dist"
}

// LoadAuthConfigFromEnv loads authentication configuration from environment variables.
// AUTH_ENABLED: "true" to enable (default: false)
// AUTH_TOKEN: Token string (required if enabled)
func LoadAuthConfigFromEnv() AuthConfig {
	enabled, _ := strconv.ParseBool(os.Getenv("AUTH_ENABLED"))
	token := os.Getenv("AUTH_TOKEN")

	return AuthConfig{
		AuthEnabled: enabled,
		AuthToken:   token,
	}
}
