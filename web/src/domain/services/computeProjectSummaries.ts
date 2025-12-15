import type { KkachiState } from '../models/KkachiState';
import type { ProjectSummary } from '../models/ProjectSummary';

/**
 * Computes project summaries from KkachiState.
 *
 * Aggregation rules:
 * 1. Project list = Union of docs_heads keys and workspaces[].project
 * 2. For each project:
 *    - docs_head: docs_heads[project] or null
 *    - workspace_count: number of workspaces for this project
 *    - unknown_count: workspace_count when docs_head is null
 *    - outdated_count: count of workspaces where docs_hash !== docs_head
 *    - last_reported_at_max: max(last_reported_at) among project workspaces
 *
 * @param state - The complete kkachi state from server
 * @returns Array of ProjectSummary sorted by project name
 */
export function computeProjectSummaries(state: KkachiState): ProjectSummary[] {
    // Collect all unique project names
    const projectNames = new Set<string>();

    // Add projects from docs_heads
    Object.keys(state.docs_heads).forEach((project) => {
        projectNames.add(project);
    });

    // Add projects from workspaces
    state.workspaces.forEach((workspace) => {
        projectNames.add(workspace.project);
    });

    // Compute summary for each project
    const summaries: ProjectSummary[] = [];

    for (const project of projectNames) {
        const docsHead = state.docs_heads[project] ?? null;
        const projectWorkspaces = state.workspaces.filter(
            (ws) => ws.project === project
        );
        const workspaceCount = projectWorkspaces.length;

        let unknownCount = 0;
        let outdatedCount = 0;
        let lastReportedAtMax: string | null = null;

        if (docsHead === null) {
            // No docs_head means all workspaces are unknown
            unknownCount = workspaceCount;
        } else {
            // Count outdated workspaces
            outdatedCount = projectWorkspaces.filter(
                (ws) => ws.docs_hash !== docsHead
            ).length;
        }

        // Compute max last_reported_at
        for (const ws of projectWorkspaces) {
            if (ws.last_reported_at !== null) {
                if (
                    lastReportedAtMax === null ||
                    ws.last_reported_at > lastReportedAtMax
                ) {
                    lastReportedAtMax = ws.last_reported_at;
                }
            }
        }

        summaries.push({
            project,
            docs_head: docsHead,
            workspace_count: workspaceCount,
            unknown_count: unknownCount,
            outdated_count: outdatedCount,
            last_reported_at_max: lastReportedAtMax,
        });
    }

    // Sort by project name ascending
    summaries.sort((a, b) => a.project.localeCompare(b.project));

    return summaries;
}

/**
 * Sorts project summaries with outdated projects first.
 * Secondary sort by project name ascending.
 *
 * @param summaries - Array of project summaries
 * @returns New sorted array
 */
export function sortByOutdatedFirst(summaries: ProjectSummary[]): ProjectSummary[] {
    return [...summaries].sort((a, b) => {
        // Outdated projects first
        if (a.outdated_count > 0 && b.outdated_count === 0) return -1;
        if (a.outdated_count === 0 && b.outdated_count > 0) return 1;

        // Then by outdated count descending
        if (a.outdated_count !== b.outdated_count) {
            return b.outdated_count - a.outdated_count;
        }

        // Then by project name
        return a.project.localeCompare(b.project);
    });
}
