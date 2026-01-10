package git

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

var ErrRepoConfigMissing = errors.New("repo_config_missing")

type GitDocsRepository struct {
	git       *Client
	stateRepo *state.FileStateRepository
}

func NewGitDocsRepository(git *Client, stateRepo *state.FileStateRepository) *GitDocsRepository {
	return &GitDocsRepository{
		git:       git,
		stateRepo: stateRepo,
	}
}

func (r *GitDocsRepository) GetHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	repoConfig, err := r.getRepoConfig(project)
	if err != nil {
		return "", err
	}

	head, err := r.git.RevParseHead(ctx, repoConfig.Path)
	if err != nil {
		return "", err
	}
	return docs.CommitHash(head), nil
}

func (r *GitDocsRepository) GetSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	repoConfig, err := r.getRepoConfig(project)
	if err != nil {
		return nil, "", err
	}

	resolvedCommit, err := r.git.ResolveCommit(ctx, repoConfig.Path, string(commit))
	if err != nil {
		if errors.Is(err, ErrUnknownCommit) {
			return nil, "", docs.ErrUnknownDocsCommit
		}
		return nil, "", err
	}

	snapshot, err := r.git.ArchiveDocs(ctx, repoConfig.Path, resolvedCommit)
	if err != nil {
		return nil, "", err
	}
	return docs.DocsSnapshot(snapshot), docs.CommitHash(resolvedCommit), nil
}

func (r *GitDocsRepository) PushSnapshot(ctx context.Context, project docs.ProjectName, base docs.CommitHash, snapshot docs.DocsSnapshot, actorEmail string) (docs.DocsPushResult, error) {
	repoConfig, err := r.getRepoConfig(project)
	if err != nil {
		return docs.DocsPushResult{}, err
	}
	repoPath := repoConfig.Path

	// 1. Fetch origin
	if err := r.git.Fetch(ctx, repoPath); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("fetch failed: %w", err)
	}

	// 2. Checkout main branch
	if err := r.git.CheckoutMain(ctx, repoPath); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("checkout main failed: %w", err)
	}

	// 3. Reset hard to origin/main
	if err := r.git.ResetHardToOriginMain(ctx, repoPath); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("reset to origin/main failed: %w", err)
	}

	// 4. Get current HEAD
	currentHead, err := r.git.RevParseHead(ctx, repoPath)
	if err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("rev-parse HEAD failed: %w", err)
	}

	// 5. Verify base commit exists
	if err := r.git.VerifyCommit(ctx, repoPath, string(base)); err != nil {
		if errors.Is(err, ErrUnknownCommit) {
			return docs.DocsPushResult{}, docs.ErrUnknownDocsCommit
		}
		return docs.DocsPushResult{}, fmt.Errorf("verify base commit failed: %w", err)
	}

	// 6. Check if base matches HEAD (outdated detection)
	if currentHead != string(base) {
		return docs.DocsPushResult{
			Status:      docs.DocsPushStatusOutdated,
			CurrentHead: docs.CommitHash(currentHead),
		}, nil
	}

	// 7. Apply snapshot to the docs repository root.
	// In the current design, the docs repo root is treated as the canonical
	// docs tree (docs repo 전체 루트가 곧 docs). The snapshot contains paths
	// relative to this root, and we mirror them into the repository while
	// leaving Git metadata (e.g., .git) intact.
	if err := r.applySnapshot(repoPath, snapshot); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("apply snapshot failed: %w", err)
	}

	// 8. Remove .DS_Store files before staging (macOS Finder metadata)
	if err := r.removeDSStore(repoPath); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("remove .DS_Store failed: %w", err)
	}

	// 9. Stage changes
	if err := r.git.AddDocs(ctx, repoPath); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("git add failed: %w", err)
	}

	// 9. Check if there are any changes
	isEmpty, err := r.git.DiffStagedIsEmpty(ctx, repoPath)
	if err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("git diff --cached failed: %w", err)
	}
	if isEmpty {
		return docs.DocsPushResult{
			Status:      docs.DocsPushStatusNoChange,
			CurrentHead: docs.CommitHash(currentHead),
		}, nil
	}

	// 10. Commit changes
	commitMsg := fmt.Sprintf("%s docs update by %s", project, actorEmail)
	if err := r.git.Commit(ctx, repoPath, commitMsg, actorEmail); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("git commit failed: %w", err)
	}

	// 11. Get new HEAD
	newHead, err := r.git.RevParseHead(ctx, repoPath)
	if err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("rev-parse HEAD after commit failed: %w", err)
	}

	newHeadHash := docs.CommitHash(newHead)
	return docs.DocsPushResult{
		Status:  docs.DocsPushStatusUpdated,
		NewHead: &newHeadHash,
	}, nil
}

func (r *GitDocsRepository) getRepoConfig(project docs.ProjectName) (config.DocsRepoConfig, error) {
	repoID, ok := r.stateRepo.GetDocsRepoID(string(project))
	if !ok {
		return config.DocsRepoConfig{}, docs.ErrUnknownProject
	}
	repoConfig, ok := r.stateRepo.GetDocsRepo(repoID)
	if !ok {
		return config.DocsRepoConfig{}, ErrRepoConfigMissing
	}
	return repoConfig, nil
}

// applySnapshot extracts a tar.gz snapshot into the specified docs root
// directory. The snapshot paths are interpreted as relative to this root.
// Existing non-hidden contents under the root are cleared before extraction,
// but Git metadata such as .git and other dot-directories/files are preserved.
func (r *GitDocsRepository) applySnapshot(docsRoot string, snapshot docs.DocsSnapshot) error {
	// Normalize docsRoot for path comparison
	absDocsRoot, err := filepath.Abs(docsRoot)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for docs root: %w", err)
	}

	// Ensure docs root exists
	if err := os.MkdirAll(docsRoot, 0755); err != nil {
		return fmt.Errorf("failed to create docs root: %w", err)
	}

	// Clear existing contents under the docs root, but preserve the Git
	// metadata directory (.git). All other entries, including hidden files
	// and directories (e.g., .github, .gitignore), are treated as part of
	// the docs tree and will be fully controlled by incoming snapshots.
	entries, err := os.ReadDir(docsRoot)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read docs root: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" {
			continue
		}
		path := filepath.Join(docsRoot, name)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove existing path %s: %w", path, err)
		}
	}

	// Decompress gzip
	gzReader, err := gzip.NewReader(bytes.NewReader(snapshot))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Extract tar
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		targetPath := header.Name
		if targetPath == "" || targetPath == "." {
			continue
		}

		// Security: Validate the target path to prevent path traversal attacks.
		// 1. Clean the path to normalize it
		cleanPath := filepath.Clean(targetPath)

		// 2. Reject absolute paths
		if filepath.IsAbs(cleanPath) {
			return fmt.Errorf("invalid tar entry: absolute path not allowed: %s", header.Name)
		}

		// 3. Reject paths with ".." as a path segment (not part of filename like "a..b.md"),
		// and skip any entries that would touch the Git metadata directory (.git).
		// We split on the OS-specific separator so that, after filepath.Clean
		// normalizes the path (which may rewrite separators on Windows), we
		// still reliably detect traversal segments.
		skipEntry := false
		for _, segment := range strings.Split(cleanPath, string(filepath.Separator)) {
			if segment == ".." {
				return fmt.Errorf("invalid tar entry: path traversal not allowed: %s", header.Name)
			}
			if segment == ".git" {
				// Never allow snapshots to create or modify .git contents.
				skipEntry = true
				break
			}
		}
		if skipEntry {
			continue
		}

		// 4. Compute final path and verify it's under docsRoot
		fullPath := filepath.Join(absDocsRoot, cleanPath)
		if !strings.HasPrefix(fullPath, absDocsRoot+string(filepath.Separator)) && fullPath != absDocsRoot {
			return fmt.Errorf("invalid tar entry: path escapes target directory: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(fullPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", fullPath, err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", fullPath, err)
			}
			file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", fullPath, err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close() // best-effort close on write error
				return fmt.Errorf("failed to write file %s: %w", fullPath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("failed to close file %s: %w", fullPath, err)
			}
		}
	}

	return nil
}

func (r *GitDocsRepository) Push(ctx context.Context, project docs.ProjectName) error {
	repoConfig, err := r.getRepoConfig(project)
	if err != nil {
		return err
	}
	return r.git.Push(ctx, repoConfig.Path)
}

// removeDSStore recursively removes .DS_Store files from the given root path.
// It skips the .git directory to avoid modifying Git metadata.
func (r *GitDocsRepository) removeDSStore(rootPath string) error {
	return filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip .git directory
		if d.Name() == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		// Remove .DS_Store files
		if d.Name() == ".DS_Store" && !d.IsDir() {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}
