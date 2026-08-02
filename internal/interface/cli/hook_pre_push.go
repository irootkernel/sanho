package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/usecase/hook"
)

// prePushTimeout is the timeout for pre-push operations.
const prePushTimeout = 2 * time.Minute

type prePushUpdate struct {
	LocalRef  string
	LocalOID  string
	RemoteRef string
	RemoteOID string
}

// runPrePushHook executes the pre-push hook logic.
func runPrePushHook(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), prePushTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho hook pre-push: failed to get current directory: %v\n", err)
		return err
	}
	config, err := fs.NewFileConfigLoader().Load(cwd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: load configuration: %w", err)
	}
	if err := retryPendingWorkspaceReport(ctx, cwd, config); err != nil {
		return fmt.Errorf("sanho hook pre-push: %w", err)
	}
	pullCommit := newPullCommitEngine(nil)
	hasPullCommit, err := pullCommit.hasTransaction(ctx, cwd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: check pull-commit state: %w", err)
	}
	if hasPullCommit {
		assessment, assessErr := pullCommit.assessTransaction(ctx, cwd)
		if assessErr != nil {
			return fmt.Errorf("sanho hook pre-push: classify pull-commit state: %w", assessErr)
		}
		cmd.PrintErrf("sanho: pull-commit transaction is %s: %s.\n", assessment.Classification, assessment.Reason)
		cmd.PrintErrf("Safe next step: %s\n", assessment.NextCommand)
		cmd.PrintErrln("Push is blocked so the docs base commit cannot be published alone.")
		return errors.New("pull-commit transaction exists - push blocked")
	}
	hasPulledDocs, err := hasPulledDocsBaseline(ctx, cwd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: check pulled docs baseline: %w", err)
	}
	if hasPulledDocs {
		cmd.PrintErrln("sanho: pulled docs have not been materialized in application history.")
		cmd.PrintErrln("Run 'sanho pull-commit' before pushing.")
		return errors.New("pulled docs baseline exists - push blocked")
	}

	// Create dependencies
	configLoader := fs.NewFileConfigLoader()
	pendingFixStore := newPrePushPendingFixStoreAdapter(fs.NewFilePendingFixStore())
	conflictDetector := merge.NewFileConflictDetector()
	output := newCLIPrePushOutput(cmd)

	// Create usecase
	usecase := hook.NewPrePushUseCase(
		configLoader,
		pendingFixStore,
		conflictDetector,
		output,
	)

	// Execute
	if err := usecase.Execute(ctx, cwd); err != nil {
		// Handle specific errors with appropriate messages
		switch {
		case errors.Is(err, hook.ErrConfigBroken):
			// Message already printed by output
		case errors.Is(err, hook.ErrPrePushConflictMarkerFound):
			// Message already printed by output
		case errors.Is(err, hook.ErrPrePushPendingFixExists):
			// Message already printed by output
		default:
			cmd.PrintErrf("sanho hook pre-push: %v\n", err)
		}
		return err
	}

	updates, err := readPrePushUpdates(cmd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: %w", err)
	}
	remoteName := ""
	if len(args) > 0 {
		remoteName = args[0]
	}
	if !hasNonDeleteBranchUpdate(updates) {
		return nil
	}
	if remoteName == "" {
		publication, assessErr := assessMainPublication(ctx, cwd, false)
		if assessErr != nil {
			return fmt.Errorf("sanho hook pre-push: inspect legacy hook publication state: %w", assessErr)
		}
		if publication.Exists {
			if installErr := infraGit.NewHookInstaller().InstallAllHooks(ctx, cwd); installErr != nil {
				return fmt.Errorf("sanho hook pre-push: upgrade legacy hook: %w", installErr)
			}
			cmd.PrintErrln("sanho: upgraded the installed pre-push hook to forward remote arguments.")
			cmd.PrintErrln("Retry the same git push.")
			return errors.New("pre-push hook upgrade required")
		}
		return nil
	}
	if remoteName != "origin" {
		return nil
	}
	return publishMainBeforeTarget(ctx, cwd, updates, cmd)
}

func publishMainBeforeTarget(
	ctx context.Context,
	workDir string,
	updates []prePushUpdate,
	cmd *cobra.Command,
) error {
	publication, err := assessMainPublication(ctx, workDir, true)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: inspect main publication: %w", err)
	}
	if !publication.Exists {
		return nil
	}
	if publication.Classification != mainPublicationPending {
		cmd.PrintErrf("sanho: origin/main publication is %s: %s.\n", publication.Classification, publication.Reason)
		return errors.New("origin/main publication is blocked")
	}
	if mainUpdate, ok := findRemoteMainUpdate(updates); ok {
		if mainUpdate.LocalOID != publication.LocalMain || mainUpdate.LocalRef != "refs/heads/main" {
			cmd.PrintErrln("sanho: pending main publication must update origin/main from the local main branch.")
			return errors.New("origin/main push does not use the local main branch")
		}
		if countBranchUpdates(updates) > 1 {
			cmd.PrintErrln("sanho: pending main publication cannot share one push with other branch updates.")
			cmd.PrintErrln("Push origin/main first, then retry the remaining refs.")
			return errors.New("origin/main must be published separately")
		}
		return nil
	}

	cmd.Printf("sanho: publishing local main %s to origin/main before the target branch.\n", shortHash(publication.LocalMain))
	if err := pushLocalMain(ctx, workDir); err != nil {
		if recordErr := recordMainPublicationFailure(ctx, workDir, err); recordErr != nil {
			err = errors.Join(err, fmt.Errorf("record publication failure: %w", recordErr))
		}
		cmd.PrintErrf("sanho: %v\n", err)
		return errors.New("origin/main publication failed - target push blocked")
	}
	publication, err = assessMainPublication(ctx, workDir, true)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: verify main publication: %w", err)
	}
	if publication.Exists {
		cmd.PrintErrf("sanho: origin/main publication remains %s: %s.\n", publication.Classification, publication.Reason)
		return errors.New("origin/main publication was not verified - target push blocked")
	}
	cmd.Println("sanho: origin/main publication completed; continuing target push.")
	return nil
}

func readPrePushUpdates(cmd *cobra.Command) ([]prePushUpdate, error) {
	updates := make([]prePushUpdate, 0)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid pre-push update line %q", scanner.Text())
		}
		updates = append(updates, prePushUpdate{
			LocalRef:  fields[0],
			LocalOID:  fields[1],
			RemoteRef: fields[2],
			RemoteOID: fields[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pre-push updates: %w", err)
	}
	return updates, nil
}

func hasNonDeleteBranchUpdate(updates []prePushUpdate) bool {
	return countNonDeleteBranchUpdates(updates) > 0
}

func countNonDeleteBranchUpdates(updates []prePushUpdate) int {
	count := 0
	for _, update := range updates {
		if strings.HasPrefix(update.RemoteRef, "refs/heads/") && !isZeroObjectID(update.LocalOID) {
			count++
		}
	}
	return count
}

func countBranchUpdates(updates []prePushUpdate) int {
	count := 0
	for _, update := range updates {
		if strings.HasPrefix(update.RemoteRef, "refs/heads/") {
			count++
		}
	}
	return count
}

func findRemoteMainUpdate(updates []prePushUpdate) (prePushUpdate, bool) {
	for _, update := range updates {
		if update.RemoteRef == "refs/heads/main" && !isZeroObjectID(update.LocalOID) {
			return update, true
		}
	}
	return prePushUpdate{}, false
}

func isZeroObjectID(value string) bool {
	return value != "" && strings.Trim(value, "0") == ""
}

// cliPrePushOutput implements hook.PrePushOutput for CLI.
type cliPrePushOutput struct {
	cmd *cobra.Command
}

func newCLIPrePushOutput(cmd *cobra.Command) *cliPrePushOutput {
	return &cliPrePushOutput{cmd: cmd}
}

func (o *cliPrePushOutput) Info(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliPrePushOutput) Warning(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliPrePushOutput) Error(msg string) {
	o.cmd.PrintErrf("sanho: %s\n", msg)
}

// prePushPendingFixStoreAdapter adapts fs.FilePendingFixStore to hook.PrePushPendingFixStore interface.
type prePushPendingFixStoreAdapter struct {
	store *fs.FilePendingFixStore
}

func newPrePushPendingFixStoreAdapter(store *fs.FilePendingFixStore) *prePushPendingFixStoreAdapter {
	return &prePushPendingFixStoreAdapter{store: store}
}

func (a *prePushPendingFixStoreAdapter) Exists(path string) (bool, error) {
	_, exists, err := a.store.Read(path)
	return exists, err
}
