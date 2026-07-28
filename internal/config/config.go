package config

type DocsRepoConfig struct {
	ID      string
	Path    string
	RepoURL string
}

type Config struct {
	DocsRepos []DocsRepoConfig
}
