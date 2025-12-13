package config

type DocsRepoConfig struct {
	ID      string
	Path    string
	RepoURL string
}

type Config struct {
	DocsRepos []DocsRepoConfig
}

// ResolveWebDistDir returns the path to the web distribution directory.
// Falls back to "web/dist" if envValue is empty.
func ResolveWebDistDir(envValue string) string {
	if envValue != "" {
		return envValue
	}
	return "web/dist"
}
