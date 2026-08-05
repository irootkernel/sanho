package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/client"
)

func validatePrePushDocsProvenance(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	updates []prePushUpdate,
	httpClient docsProvenanceHTTPClient,
	cmd *cobra.Command,
) error {
	verifier := newDocsProvenanceVerifier(httpClient)
	results := make(map[string]docsProvenanceResult)
	invalid := make([]string, 0)
	for _, update := range updates {
		if !strings.HasPrefix(update.RemoteRef, "refs/heads/") || isZeroObjectID(update.LocalOID) {
			continue
		}
		result, ok := results[update.LocalOID]
		if !ok {
			var err error
			result, err = verifier.Verify(ctx, workDir, update.LocalOID, config)
			if err != nil {
				return fmt.Errorf(
					"sanho hook pre-push: validate docs provenance for %s (%s): %w",
					update.RemoteRef,
					shortHash(update.LocalOID),
					err,
				)
			}
			results[update.LocalOID] = result
		}
		if result.Valid {
			continue
		}
		cmd.PrintErrf(
			"sanho: proposed branch %s at %s has invalid docs provenance (%s): %s.\n",
			update.RemoteRef,
			shortHash(update.LocalOID),
			result.Classification,
			result.Reason,
		)
		invalid = append(invalid, update.RemoteRef)
	}
	if len(invalid) > 0 {
		cmd.PrintErrln("sanho: push blocked before any remote ref was changed.")
		return errors.New("one or more proposed branch tips have invalid docs provenance")
	}
	return nil
}

func proposedBranchIncludesOID(updates []prePushUpdate, oid string) bool {
	for _, update := range updates {
		if strings.HasPrefix(update.RemoteRef, "refs/heads/") &&
			!isZeroObjectID(update.LocalOID) && update.LocalOID == oid {
			return true
		}
	}
	return false
}
