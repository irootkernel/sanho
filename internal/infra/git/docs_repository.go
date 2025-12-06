package git

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
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

	// 7. Apply snapshot to docs/ directory
	docsDir := filepath.Join(repoPath, "docs")
	if err := r.applySnapshot(docsDir, snapshot); err != nil {
		return docs.DocsPushResult{}, fmt.Errorf("apply snapshot failed: %w", err)
	}

	// 8. Stage changes
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

// applySnapshot extracts a tar.gz snapshot to the docs directory.
// It first removes the existing docs directory, then extracts the snapshot.
func (r *GitDocsRepository) applySnapshot(docsDir string, snapshot docs.DocsSnapshot) error {
	// Normalize docsDir for path comparison
	absDocsDir, err := filepath.Abs(docsDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for docs dir: %w", err)
	}

	// Remove existing docs directory
	if err := os.RemoveAll(docsDir); err != nil {
		return fmt.Errorf("failed to remove existing docs dir: %w", err)
	}

	// Create docs directory
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return fmt.Errorf("failed to create docs dir: %w", err)
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

		// The tar might contain "docs/" prefix, we need to strip it
		// since we're extracting into the docs directory
		targetPath := header.Name
		if strings.HasPrefix(targetPath, "docs/") {
			targetPath = targetPath[len("docs/"):]
		}

		if targetPath == "" || targetPath == "." {
			continue
		}

		// Security: Validate the target path to prevent path traversal attacks
		// 1. Clean the path to normalize it
		cleanPath := filepath.Clean(targetPath)

		// 2. Reject absolute paths
		if filepath.IsAbs(cleanPath) {
			return fmt.Errorf("invalid tar entry: absolute path not allowed: %s", header.Name)
		}

		// 3. Reject paths with ".." as a path segment (not part of filename like "a..b.md")
		for _, segment := range strings.Split(cleanPath, string(filepath.Separator)) {
			if segment == ".." {
				return fmt.Errorf("invalid tar entry: path traversal not allowed: %s", header.Name)
			}
		}

		// 4. Compute final path and verify it's under docsDir
		fullPath := filepath.Join(absDocsDir, cleanPath)
		if !strings.HasPrefix(fullPath, absDocsDir+string(filepath.Separator)) && fullPath != absDocsDir {
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
