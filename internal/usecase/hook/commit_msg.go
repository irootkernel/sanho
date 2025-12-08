package hook

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/domain/client"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

// CommitMsgConfigLoader loads workspace configuration.
type CommitMsgConfigLoader interface {
	Load(workDir string) (*client.WorkspaceConfig, error)
}

// CommitMsgDocsHashStore reads docs hash files.
type CommitMsgDocsHashStore interface {
	Read(path string) (docs.CommitHash, error)
}

// CommitMsgGitClient provides git operations.
type CommitMsgGitClient interface {
	HasDocsChangeStaged(ctx context.Context, repoPath, docsDir string) (bool, error)
}

// CommitMsgOutput is the output callback for user messages.
type CommitMsgOutput interface {
	Info(msg string)
	Warning(msg string)
}

// CommitMsgUseCase handles the commit-msg hook logic.
type CommitMsgUseCase struct {
	configLoader  CommitMsgConfigLoader
	docsHashStore CommitMsgDocsHashStore
	gitClient     CommitMsgGitClient
	output        CommitMsgOutput
}

// NewCommitMsgUseCase creates a new CommitMsgUseCase.
func NewCommitMsgUseCase(
	configLoader CommitMsgConfigLoader,
	docsHashStore CommitMsgDocsHashStore,
	gitClient CommitMsgGitClient,
	output CommitMsgOutput,
) *CommitMsgUseCase {
	return &CommitMsgUseCase{
		configLoader:  configLoader,
		docsHashStore: docsHashStore,
		gitClient:     gitClient,
		output:        output,
	}
}

// Execute runs the commit-msg hook logic.
// msgFilePath is the path to the commit message file (passed by Git as $1).
func (u *CommitMsgUseCase) Execute(ctx context.Context, workDir, msgFilePath string) error {
	// Step 1: Load configuration
	config, err := u.configLoader.Load(workDir)
	if err != nil {
		// If config doesn't exist, silently skip (not a kkachi workspace)
		u.output.Warning("Not a kkachi workspace, skipping docs-version tag.")
		return nil
	}
	config.ApplyDefaults()

	// Step 2: Check for staged docs changes
	hasChanges, err := u.gitClient.HasDocsChangeStaged(ctx, workDir, config.DocsDir)
	if err != nil {
		u.output.Warning(fmt.Sprintf("Failed to check docs changes: %v", err))
		return nil // Don't block commit
	}
	if !hasChanges {
		// No docs changes, no need to add tag
		return nil
	}

	// Step 3: Check if docs-version already exists in message
	msgContent, err := os.ReadFile(msgFilePath)
	if err != nil {
		u.output.Warning(fmt.Sprintf("Failed to read commit message file: %v", err))
		return nil // Don't block commit
	}

	if hasDocsVersionTag(string(msgContent)) {
		// Tag already exists, no need to add
		return nil
	}

	// Step 4: Read docs hash
	hashFilePath := filepath.Join(workDir, config.DocsHashFile)
	docsHash, err := u.docsHashStore.Read(hashFilePath)
	if err != nil {
		u.output.Warning(fmt.Sprintf("Failed to read docs hash: %v", err))
		return nil // Don't block commit
	}

	// Step 5: Append docs-version tag to message
	newContent := appendDocsVersionTag(string(msgContent), string(docsHash))
	if err := os.WriteFile(msgFilePath, []byte(newContent), 0644); err != nil {
		u.output.Warning(fmt.Sprintf("Failed to write commit message: %v", err))
		return nil // Don't block commit
	}

	u.output.Info(fmt.Sprintf("Added docs-version: %s to commit message.", docsHash))
	return nil
}

// hasDocsVersionTag checks if the commit message already contains a docs-version tag.
func hasDocsVersionTag(content string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "docs-version:") {
			return true
		}
	}
	return false
}

// appendDocsVersionTag appends the docs-version tag to the commit message.
// It adds a blank line before the tag if the message doesn't end with one.
func appendDocsVersionTag(content, hash string) string {
	// Trim trailing whitespace/newlines
	content = strings.TrimRight(content, "\n\r\t ")

	// Add blank line if content is not empty
	if len(content) > 0 {
		content += "\n\n"
	}

	// Add docs-version tag
	content += fmt.Sprintf("docs-version: %s\n", hash)

	return content
}
