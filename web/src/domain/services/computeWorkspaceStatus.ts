import type { Status } from '../models/Status';
import type { Workspace } from '../models/Workspace';


/**
 * Computes the synchronization status of a workspace.
 *
 * Rules:
 * - If docsHead is null, status is 'unknown'.
 * - If workspace.docs_hash matches docsHead, status is 'up_to_date'.
 * - Otherwise, status is 'outdated'.
 *
 * @param workspace - The workspace to check
 * @param docsHead - The current HEAD hash of the docs repository for the project (or null if not found)
 * @returns Status ('up_to_date' | 'outdated' | 'unknown')
 */
export function computeWorkspaceStatus(
    workspace: Workspace,
    docsHead: string | null
): Status {
    if (docsHead === null) {
        return 'unknown';
    }

    if (workspace.docs_hash === docsHead) {
        return 'up_to_date';
    }

    return 'outdated';
}
