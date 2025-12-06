package client

import (
	"net/url"
	"path"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

// ExtractDocsRepoID extracts the repository name from a Git URL and returns it as DocsRepoID.
// This function handles both SSH and HTTPS Git URLs.
//
// Examples:
//   - git@github.com:SeventeenthEarth/sudal_docs.git -> sudal_docs
//   - https://github.com/SeventeenthEarth/sudal_docs.git -> sudal_docs
//   - https://github.com/SeventeenthEarth/sudal_docs -> sudal_docs
func ExtractDocsRepoID(gitURL string) docs.DocsRepoID {
	if gitURL == "" {
		return ""
	}

	var repoPath string

	// Handle SSH URL format: git@host:path/repo.git
	if strings.HasPrefix(gitURL, "git@") {
		colonIdx := strings.Index(gitURL, ":")
		if colonIdx != -1 {
			repoPath = gitURL[colonIdx+1:]
		}
	} else {
		// Handle HTTPS URL format
		parsed, err := url.Parse(gitURL)
		if err != nil {
			return ""
		}
		repoPath = parsed.Path
	}

	// Extract the base name (repository name)
	baseName := path.Base(repoPath)

	// Remove .git suffix if present
	baseName = strings.TrimSuffix(baseName, ".git")

	return docs.DocsRepoID(baseName)
}
