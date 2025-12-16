import type { Workspace } from '../models/Workspace';
import type { Status } from '../models/Status';
import { computeWorkspaceStatus } from './computeWorkspaceStatus';

export type SortField = 'last_reported_at' | 'local_path';
export type SortDirection = 'asc' | 'desc';
export type StatusFilter = 'all' | Status;

export interface SortOption {
    field: SortField;
    direction: SortDirection;
}

export interface WorkspaceWithStatus extends Workspace {
    status: Status;
}

/**
 * Filter and sort workspaces for the Project Detail Page.
 *
 * @param workspaces - List of workspaces
 * @param docsHead - The docs HEAD for the project
 * @param filterStatus - Status to filter by ('all' or specific status)
 * @param searchQuery - Search string (matches workspace_id, local_path, repo_url)
 * @param sort - Sort option
 * @returns Filtered and sorted list of workspaces with computed status
 */
export function filterAndSortWorkspaces(
    workspaces: Workspace[],
    docsHead: string | null,
    filterStatus: StatusFilter,
    searchQuery: string,
    sort: SortOption
): WorkspaceWithStatus[] {
    // 1. Compute status and attach it
    let result: WorkspaceWithStatus[] = workspaces.map((ws) => ({
        ...ws,
        status: computeWorkspaceStatus(ws, docsHead),
    }));

    // 2. Filter by Status
    if (filterStatus !== 'all') {
        result = result.filter((ws) => ws.status === filterStatus);
    }

    // 3. Filter by Search Query (case-insensitive)
    const query = searchQuery.trim().toLowerCase();
    if (query) {
        result = result.filter(
            (ws) =>
                ws.workspace_id.toLowerCase().includes(query) ||
                ws.local_path.toLowerCase().includes(query) ||
                (ws.repo_url && ws.repo_url.toLowerCase().includes(query))
        );
    }

    // 4. Sort
    result.sort((a, b) => {
        let cmp = 0;
        switch (sort.field) {
            case 'last_reported_at': {
                // Handle nulls: null is considered "oldest" (smallest)
                const tA = a.last_reported_at ?? '';
                const tB = b.last_reported_at ?? '';
                cmp = tA.localeCompare(tB);
                break;
            }
            case 'local_path':
                cmp = a.local_path.localeCompare(b.local_path);
                break;
        }
        return sort.direction === 'asc' ? cmp : -cmp;
    });

    return result;
}
