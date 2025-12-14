import type { Workspace } from './Workspace';

/**
 * KkachiState represents the complete state returned by /api/state endpoint.
 * This is the root state model for the kkachi-web application.
 */
export interface KkachiState {
    /**
     * Map of project names to their current docs HEAD commit hash.
     * Key: project name (e.g., "sudal")
     * Value: commit hash (e.g., "abc123...")
     */
    docs_heads: Record<string, string>;

    /**
     * List of all registered workspaces across all projects.
     */
    workspaces: Workspace[];
}

/**
 * Check if the state is completely empty (no projects or workspaces).
 */
export function isEmptyState(state: KkachiState): boolean {
    return (
        Object.keys(state.docs_heads).length === 0 &&
        state.workspaces.length === 0
    );
}
